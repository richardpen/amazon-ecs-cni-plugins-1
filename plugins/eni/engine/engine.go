// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package engine

import (
	"fmt"
	"net"
	"strings"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/cninswrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/ec2metadata"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/execwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/ioutilwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/netlinkwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/oswrapper"
	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
)

const (
	metadataNetworkInterfacesPath               = "network/interfaces/macs/"
	metadataNetworkInterfaceIDPathSuffix        = "interface-id"
	sysfsPathForNetworkDevices                  = "/sys/class/net/"
	sysfsPathForNetworkDeviceAddressSuffix      = "/address"
	metadataNetworkInterfaceIPV4CIDRPathSuffix  = "/subnet-ipv4-cidr-block"
	metadataNetworkInterfaceIPV4AddressesSuffix = "/local-ipv4s"
)

// Engine represents the execution engine for the ENI plugin. It defines all the
// operations performed by the plugin
type Engine interface {
	GetAllMACAddresses() ([]string, error)
	GetMACAddressOfENI(macAddresses []string, eniID string) (string, error)
	GetInterfaceDeviceName(macAddress string) (string, error)
	GetIPV4GatewayNetmask(macAddress string) (string, string, error)
	DoesMACAddressMapToIPV4Address(macAddress string, ipv4Address string) (bool, error)
	SetupContainerNamespace(netns string, deviceName string, ipv4Address string, netmask string) error
	IsDHClientInPath() bool
	TeardownContainerNamespace(netns string, macAddress string) error
}

type engine struct {
	metadata ec2metadata.EC2Metadata
	ioutil   ioutilwrapper.IOUtil
	netLink  netlinkwrapper.NetLink
	ns       cninswrapper.NS
	exec     execwrapper.Exec
	os       oswrapper.OS
}

// New creates a new Engine object
func New() Engine {
	return create(
		ec2metadata.NewEC2Metadata(), ioutilwrapper.NewIOUtil(), netlinkwrapper.NewNetLink(), cninswrapper.NewNS(), execwrapper.NewExec(), oswrapper.NewOS())
}

func create(metadata ec2metadata.EC2Metadata, ioutil ioutilwrapper.IOUtil, netLink netlinkwrapper.NetLink, ns cninswrapper.NS, exec execwrapper.Exec, os oswrapper.OS) Engine {
	return &engine{
		metadata: metadata,
		ioutil:   ioutil,
		netLink:  netLink,
		ns:       ns,
		exec:     exec,
		os:       os,
	}
}

// IsDHClientInPath returns true if the 'dhclient' executable is found in PATH. It
// returns false otherwise
func (engine *engine) IsDHClientInPath() bool {
	dhclientPath, err := engine.exec.LookPath(dhclientExecutableName)
	if err != nil {
		log.Warnf("Error searching dhclient in PATH: %v", err)
		return false
	}

	log.Debugf("dhclient found in: %s", dhclientPath)
	return true
}

// GetAllMACAddresses gets a list of mac addresses for all interfaces from the instance
// metadata service
func (engine *engine) GetAllMACAddresses() ([]string, error) {
	macs, err := engine.metadata.GetMetadata(metadataNetworkInterfacesPath)
	if err != nil {
		return nil, errors.Wrap(err,
			"getAllMACAddresses engine: unable to get all mac addresses on the instance from instance metadata")
	}
	return strings.Split(macs, "\n"), nil
}

// GetMACAddressOfENI gets the mac address for a given ENI ID
func (engine *engine) GetMACAddressOfENI(macAddresses []string, eniID string) (string, error) {
	for _, macAddress := range macAddresses {
		// TODO Use fmt.Sprintf and wrap that in a method
		interfaceID, err := engine.metadata.GetMetadata(metadataNetworkInterfacesPath + macAddress + metadataNetworkInterfaceIDPathSuffix)
		if err != nil {
			log.Warnf("Error getting interface id for mac address '%s': %v", macAddress, err)
			continue
		}
		if interfaceID == eniID {
			// MAC addresses retrieved from the metadata service end with the '/' character. Strip it off.
			return strings.Split(macAddress, "/")[0], nil
		}
	}

	return "", newUnmappedMACAddressError("getMACAddressOfENI", "engine",
		fmt.Sprintf("mac address of ENI '%s' not found", eniID))
}

// GetInterfaceDeviceName gets the device name on the host, given a mac address
func (engine *engine) GetInterfaceDeviceName(macAddress string) (string, error) {
	// TODO: Use LinkList for this
	files, err := engine.ioutil.ReadDir(sysfsPathForNetworkDevices)
	if err != nil {
		return "", errors.Wrap(err,
			"getInterfaceDeviceName engine: error listing network devices from sys fs")
	}
	for _, file := range files {
		// Read the 'address' file in sys for the device. An example here is: if reading for device
		// 'eth1', read the '/sys/class/net/eth1/address' file to get the address of the device
		// TODO Use fmt.Sprintf and wrap that in a method
		addressFile := sysfsPathForNetworkDevices + file.Name() + sysfsPathForNetworkDeviceAddressSuffix
		contents, err := engine.ioutil.ReadFile(addressFile)
		if err != nil {
			log.Warnf("Error reading contents of the address file for device '%s': %v", file.Name(), err)
			continue
		}
		if strings.Contains(string(contents), macAddress) {
			return file.Name(), nil
		}
	}

	return "", newUnmappedDeviceNameError("getInterfaceDeviceName", "engine",
		fmt.Sprintf("network device name not found for mac address '%s'", macAddress))
}

// GetIPV4GatewayNetmask gets the ipv4 gateway and the netmask from the instance
// metadata, given a mac address
func (engine *engine) GetIPV4GatewayNetmask(macAddress string) (string, string, error) {
	// TODO Use fmt.Sprintf and wrap that in a method
	cidrBlock, err := engine.metadata.GetMetadata(metadataNetworkInterfacesPath + macAddress + metadataNetworkInterfaceIPV4CIDRPathSuffix)
	if err != nil {
		return "", "", errors.Wrapf(err,
			"getIPV4GatewayNetmask engine: unable to get ipv4 subnet and cidr block for '%s' from instance metadata", macAddress)
	}

	return getIPV4GatewayNetmask(cidrBlock)
}

func getIPV4GatewayNetmask(cidrBlock string) (string, string, error) {
	// The IPV4 CIDR block is of the format ip-addr/netmask
	ip, ipNet, err := net.ParseCIDR(cidrBlock)
	if err != nil {
		return "", "", errors.Wrapf(err,
			"getIPV4GatewayNetmask engine: unable to parse response for ipv4 cidr: '%s' from instance metadata", cidrBlock)
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return "", "", newParseIPV4GatewayNetmaskError("getIPV4GatewayNetmask", "engine",
			fmt.Sprintf("unable to parse ipv4 gateway from cidr block '%s'", cidrBlock))
	}

	ip4[3] = ip4[3] + 1
	maskOnes, _ := ipNet.Mask.Size()
	return ip4.String(), fmt.Sprintf("%d", maskOnes), nil
}

// DoesMACAddressMapToIPV4Address validates in the MAC Address for the ENI maps to the
// IPV4 Address specified
func (engine *engine) DoesMACAddressMapToIPV4Address(macAddress string, ipv4Address string) (bool, error) {
	// TODO Use fmt.Sprintf and wrap that in a method
	addressesResponse, err := engine.metadata.GetMetadata(metadataNetworkInterfacesPath + macAddress + metadataNetworkInterfaceIPV4AddressesSuffix)
	if err != nil {
		return false, errors.Wrap(err,
			"doesMACAddressMapToIPV4Address engine: unable to get ipv4 addresses from instance metadata")
	}
	for _, address := range strings.Split(addressesResponse, "\n") {
		if address == ipv4Address {
			return true, nil
		}
	}
	return false, nil
}

// SetupContainerNamespace configures the network namespace of the container with
// the ipv4 address and routes to use the ENI interface
func (engine *engine) SetupContainerNamespace(netns string, deviceName string, ipv4Address string, netmask string) error {
	// Get the device link for the ENI
	eniLink, err := engine.netLink.LinkByName(deviceName)
	if err != nil {
		return errors.Wrapf(err,
			"setupContainerNamespace engine: unable to get link for device '%s'", deviceName)
	}

	// Get the handle for the container's network namespace
	containerNS, err := engine.ns.GetNS(netns)
	if err != nil {
		return errors.Wrapf(err,
			"setupContainerNamespace engine: unable to get network namespace for '%s'", netns)
	}

	// Assign the ENI device to container's network namespace
	err = engine.netLink.LinkSetNsFd(eniLink, int(containerNS.Fd()))
	if err != nil {
		return errors.Wrapf(err,
			"setupContainerNamespace engine: unable to move device '%s' to container namespace '%s'", deviceName, netns)
	}

	// Generate the closure to execute within the container's namespace
	toRun, err := newSetupNamespaceClosure(engine.netLink, engine.exec, deviceName, ipv4Address, netmask)
	if err != nil {
		return errors.Wrap(err,
			"setupContainerNamespace engine: unable to create closure to execute in container namespace")
	}

	// Execute the closure within the container's namespace
	err = engine.ns.WithNetNSPath(netns, toRun.run)
	if err != nil {
		return errors.Wrapf(err,
			"setupContainerNamespace engine: unable to setup device '%s' in namespace '%s'", deviceName, netns)
	}
	return nil
}

// TeardownContainerNamespace brings down the ENI device in the container's namespace
func (engine *engine) TeardownContainerNamespace(netns string, macAddress string) error {
	// Generate the closure to execute within the container's namespace
	toRun, err := newTeardownNamespaceClosure(engine.netLink, engine.ioutil, engine.os, macAddress)
	if err != nil {
		return errors.Wrap(err,
			"teardownContainerNamespace engine: unable to create closure to execute in container namespace")
	}

	// Execute the closure within the container's namespace
	err = engine.ns.WithNetNSPath(netns, toRun.run)
	if err != nil {
		return errors.Wrap(err,
			"teardownContainerNamespace engine: unable to teardown container namespace")
	}
	return nil
}
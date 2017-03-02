package types

import (
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/stretchr/testify/assert"
)

var validConfig = `{"eni":"eni1", "ipv4-address":"10.11.12.13", "mac":"01:23:45:67:89:ab"}`
var configNoENIID = `{"ipv4-address":"10.11.12.13", "mac":"01:23:45:67:89:ab"}`
var configNoIPV4Address = `{"eni":"eni1", "mac":"01:23:45:67:89:ab"}`
var configNoMAC = `{"eni":"eni1", "ipv4-address":"10.11.12.13"}`
var configInvalidIPV4AddressMalformed = `{"eni":"eni1", "ipv4-address":"1", "mac":"01:23:45:67:89:ab"}`
var configInvalidIPV4AddressIPv6 = `{"eni":"eni1", "ipv4-address":"2001:db8::68", "mac":"01:23:45:67:89:ab"}`
var configMalformedMAC = `{"eni":"eni1", "ipv4-address":"10.11.12.13", "mac":"01:23:45:67:89"}`

func TestNewConfWithValidConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(validConfig),
	}
	_, err := NewConf(args)
	assert.NoError(t, err)
}

func TestNewConfWithEmptyConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(""),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(`{"foo":"eni1"}`),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithMissingENIIDConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configNoENIID),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithMissingIPV4AddressConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configNoIPV4Address),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithMissingMACConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configNoMAC),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidMalformedIPV4AddressConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configInvalidIPV4AddressMalformed),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidIPV4AddressIPV6Config(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configInvalidIPV4AddressIPv6),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidMACConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configMalformedMAC),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}
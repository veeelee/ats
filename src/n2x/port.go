package n2x

import (
	"fmt"
	"log"
	"strings"
)

type PortMediaType int

type Port struct {
	Name    string
	Handler string
	*NSession
	MediaTypes       map[PortMediaType]string
	MediaType        PortMediaType
	LegacyLinkSutMac string
	LegacyLinkSutIP  map[string]string
	AddressPools     map[string]*AddressPool
	LegacyLinkSutIP6 map[string]string
	AddressPools6    map[string]*AddressPool
}

const (
	RJ45 = iota
	SFP
)

func (pmt PortMediaType) String() string {
	if pmt == RJ45 {
		return "AGT_MEDIA_RJ45"
	} else if pmt == SFP {
		return "AGT_MEDIA_SFP"
	} else {
		return "UNKNOWN"
	}
}

func (p *Port) LegacyLinkGetAllAddressPools() ([]*AddressPool, error) {
	cmd := fmt.Sprintf("AgtEthernetAddresses ListAddressPools %s", p.Handler)
	res, err := p.Invoke(cmd)
	if err != nil {
		return nil, fmt.Errorf("Cannot get  ip address pools of port: %s : %s", p.Name, err.Error())
	}

	res = strings.Replace(res, "{", "", -1)
	res = strings.Replace(res, "}", "", -1)
	res = strings.TrimSpace(res)
	fields := strings.Split(res, " ")

	/*Direct drop existed entries */
	p.AddressPools = make(map[string]*AddressPool, 2)

	pools := make([]*AddressPool, 0, 2)

	for i, field := range fields {
		if strings.TrimSpace(field) != "" {
			np := &AddressPool{
				Handler: field,
				Family:  ADDRESS_FAMILY_IPV4,
				Type:    ADDRESS_POOL_LEGACY,
				Port:    p,
			}
			if i == 0 {
				np.Default = true
			} else {
				np.Default = false
			}

			err := np.GetTesterIPAddresses()
			if err != nil {
				return nil, fmt.Errorf("Cannot restore test IP address %s", err)
			}
			pools = append(pools, np)
			p.AddressPools[field] = np
		}
	}

	return pools, nil
}

func (p *Port) LegacyLinkGetAllIP6AddressPools() ([]*AddressPool, error) {
	cmd := fmt.Sprintf("AgtEthernetIpv6Addresses2 ListAddressPools %s", p.Handler)
	res, err := p.Invoke(cmd)
	if err != nil {
		return nil, fmt.Errorf("Cannot get ipv6 address pools of port: %s : %s", p.Name, err.Error())
	}

	res = strings.Replace(res, "{", "", -1)
	res = strings.Replace(res, "}", "", -1)
	res = strings.TrimSpace(res)
	fields := strings.Split(res, " ")

	/*Direct drop existed entries */
	p.AddressPools6 = make(map[string]*AddressPool, 2)

	pools := make([]*AddressPool, 0, 2)

	for i, field := range fields {
		if strings.TrimSpace(field) != "" {
			np := &AddressPool{
				Handler: field,
				Family:  ADDRESS_FAMILY_IPV6,
				Type:    ADDRESS_POOL_LEGACY,
				Port:    p,
			}
			if i == 0 {
				np.Default = true
			} else {
				np.Default = false
			}

			err := np.GetTesterIP6Addresses()
			if err != nil {
				return nil, fmt.Errorf("Cannot restore test IP6 address %s", err)
			}
			pools = append(pools, np)
			p.AddressPools6[field] = np
		}
	}

	return pools, nil
}

func (p *Port) LegacyLinkRemoveAllAddressPools() error {
	pools, err := p.LegacyLinkGetAllAddressPools()
	if err != nil {
		return fmt.Errorf("Cannot delete all address pools on port: %s: %s", p.Name, err)
	}

	for _, pool := range pools {
		if !pool.Default {
			err := p.LegacyLinkRemoveAddressPool(pool.Handler)
			if err != nil {
				return fmt.Errorf("Cannot delete address pool: %s:%s with: %s", pool.Handler, pool.First, err)
			}

			delete(p.AddressPools, pool.Handler)
		}
	}

	return nil
}

func (p *Port) LegacyLinkRemoveAllIP6AddressPools() error {
	pools, err := p.LegacyLinkGetAllIP6AddressPools()
	if err != nil {
		return fmt.Errorf("Cannot delete all ipv6 address pools on port: %s: %s", p.Name, err)
	}

	for _, pool := range pools {
		if !pool.Default {
			err := p.LegacyLinkRemoveIP6AddressPool(pool.Handler)
			if err != nil {
				return fmt.Errorf("Cannot delete ipv6 address pool: %s:%s with: %s", pool.Handler, pool.First, err)
			}

			delete(p.AddressPools6, pool.Handler)
		}
	}

	return nil
}

func (p *Port) LegacyLinkSet(vid, testerip, tplen, testmac, sutip string) error {
	err := p.LegacyLinkReset()
	if err != nil {
		return fmt.Errorf("Cannot clear the previous configuration on port: %s: %s", p.Name, err)
	}

	//Use the default pool, no need to create a new one.
	/*
		handler, err := p.LegacyLinkAddAddressPool(testerip, tplen, "1", "1")
		if err != nil {
			return fmt.Errorf("Cannot set test ip address for port %s with: %s", p.Name, err)
		}
	*/
	err = p.LegacyLinkAddSutIPAddress(sutip)
	if err != nil {
		return fmt.Errorf("Cannot set sut ip address for port %s with: %s", p.Name, err)
	}

	pool, err := p.LegacyLinkGetDefaultAddressPool()
	if err != nil {
		return fmt.Errorf("Cannot set sut ip address for port %s with: %s", p.Name, err)
	}

	err = pool.SetTesterAddress(vid, testerip, tplen, testmac)
	if err != nil {
		return fmt.Errorf("Cannot set tester ip address for port %s with: %s", p.Name, err)
	}

	return nil
}

func (p *Port) LegacyLinkGetDefaultAddressPool() (*AddressPool, error) {
	if len(p.AddressPools) == 0 {
		return nil, fmt.Errorf("There is no pool found on port: %s", p.Name)
	}

	for _, pool := range p.AddressPools {
		if pool.Default {
			return pool, nil
		}
	}

	return nil, fmt.Errorf("There is no default pool on port: %s", p.Name)
}

func (p *Port) LegacyLinkGetDefaultIP6AddressPool() (*AddressPool, error) {
	if len(p.AddressPools6) == 0 {
		return nil, fmt.Errorf("There is no ipv6 pool found on port: %s", p.Name)
	}

	for _, pool := range p.AddressPools6 {
		if pool.Default {
			return pool, nil
		}
	}

	return nil, fmt.Errorf("There is no default ipv6 pool on port: %s", p.Name)
}

func (p *Port) LegacyLinkRemoveAllSutIPAddresses() error {
	ips, err := p.LegacyLinkGetSutIPAddresses()
	if err != nil {
		return fmt.Errorf("Cannot reset sut ip on port: %s %s", p.Name, err)
	}

	for _, ip := range ips {
		err = p.LegacyLinkRemoveSutIPAddress(ip)
		if err != nil {
			return fmt.Errorf("Cannot delet sut ip %s on port: %s %s", ip, p.Name, err)
		}

		delete(p.LegacyLinkSutIP, ip)
	}

	return nil
}

func (p *Port) LegacyLinkReset() error {
	err := p.LegacyLinkRemoveAllSutIPAddresses()
	if err != nil {
		return err
	}
	return p.LegacyLinkRemoveAllAddressPools()
}

func (p *Port) LegacyLinkAddAddressPool(ip, plen, count, step string) (string, error) {
	cmd := fmt.Sprintf("AgtEthernetAddresses AddAddressPool %s %s %s %s %s", p.Handler, ip, plen, count, step)
	res, err := p.Invoke(cmd)
	if err != nil {
		return "", fmt.Errorf("Cannot add ipv6 address pools of port: %s : %s", p.Name, err.Error())
	}

	np := &AddressPool{
		Handler: strings.TrimSpace(res),
		Family:  ADDRESS_FAMILY_IPV4,
		Type:    ADDRESS_POOL_LEGACY,
		Port:    p,
		First:   ip,
		Plen:    plen,
		Count:   count,
		Step:    step,
		Default: false,
	}

	if p.AddressPools == nil {
		p.AddressPools = make(map[string]*AddressPool, 2)
	}

	p.AddressPools[np.Handler] = np

	return np.Handler, nil
}

//AgtEthernetIpv6Addresses2 AddAddressPool
func (p *Port) LegacyLinkAddIP6AddressPool(ip, plen, count, step string) (string, error) {
	cmd := fmt.Sprintf("AgtEthernetIpv6Addresses2 AddAddressPool %s %s %s %s %s", p.Handler, ip, plen, count, step)
	res, err := p.Invoke(cmd)
	if err != nil {
		return "", fmt.Errorf("Cannot add ipv6 address pools of port: %s : %s", p.Name, err.Error())
	}

	np := &AddressPool{
		Handler: strings.TrimSpace(res),
		Family:  ADDRESS_FAMILY_IPV6,
		Type:    ADDRESS_POOL_LEGACY,
		Port:    p,
		First:   ip,
		Plen:    plen,
		Count:   count,
		Step:    step,
		Default: false,
	}

	if p.AddressPools6 == nil {
		p.AddressPools6 = make(map[string]*AddressPool, 2)
	}

	p.AddressPools6[np.Handler] = np

	return np.Handler, nil
}

func (p *Port) LegacyLinkRemoveAddressPool(handler string) error {
	cmd := fmt.Sprintf("AgtEthernetAddresses RemoveAddressPool %s %s", p.Handler, handler)
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot remove ip address pools of port: %s : %s", p.Name, err.Error())
	}

	return nil
}

//AgtEthernetIpv6Addresses2 RemoveAddressPool
func (p *Port) LegacyLinkRemoveIP6AddressPool(handler string) error {
	cmd := fmt.Sprintf("AgtEthernetIpv6Addresses2 RemoveAddressPool %s %s", p.Handler, handler)
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot remove ipv6 address pools of port: %s : %s", p.Name, err.Error())
	}

	return nil
}

func (p *Port) LegacyLinkRemoveAddressPoolByIP(ip string) error {
	var handler string
	for h, pool := range p.AddressPools {
		if pool.First == strings.TrimSpace(ip) {
			handler = h
			break
		}
	}

	if handler == "" {
		return fmt.Errorf("Cannot find address pool with: %s on port: %s", ip, p.Name)
	}

	return p.LegacyLinkRemoveAddressPool(handler)
}

func (p *Port) LegacyLinkRemoveIP6AddressPoolByIP(ip string) error {
	var handler string
	for h, pool := range p.AddressPools6 {
		if pool.First == strings.TrimSpace(ip) {
			handler = h
			break
		}
	}

	if handler == "" {
		return fmt.Errorf("Cannot find ipv6 address pool with: %s on port: %s", ip, p.Name)
	}

	return p.LegacyLinkRemoveIP6AddressPool(handler)
}

func (p *Port) LegacyLinkRemoveAddressPoolBySutIP(ip string) error {
	var handler string
	for h, pool := range p.AddressPools {
		if pool.Sut == strings.TrimSpace(ip) {
			handler = h
			break
		}
	}

	if handler == "" {
		return fmt.Errorf("Cannot find address pool with sut: %s on port: %s", ip, p.Name)
	}

	return p.LegacyLinkRemoveAddressPool(handler)
}

func (p *Port) LegacyLinkRemoveIP6AddressPoolBySutIP(ip string) error {
	var handler string
	for h, pool := range p.AddressPools6 {
		if pool.Sut == strings.TrimSpace(ip) {
			handler = h
			break
		}
	}

	if handler == "" {
		return fmt.Errorf("Cannot find ipv6 address pool with sut: %s on port: %s", ip, p.Name)
	}

	return p.LegacyLinkRemoveIP6AddressPool(handler)
}

func (p *Port) LegacyLinkGetSutIPAddresses() ([]string, error) {
	cmd := fmt.Sprintf("AgtEthernetAddresses ListSutIpAddresses %s", p.Handler)
	res, err := p.Invoke(cmd)
	if err != nil {
		return nil, fmt.Errorf("Cannot get sup ip address : %s", err.Error())
	}

	/*If previous entries exist, drop it */
	p.LegacyLinkSutIP = make(map[string]string, 1)

	res = strings.Replace(res, "{", "", -1)
	res = strings.Replace(res, "}", "", -1)
	fields := strings.Split(res, " ")

	ips := make([]string, 0, 10)
	for _, field := range fields {
		ips = append(ips, strings.TrimSpace(field))
		p.LegacyLinkSutIP[strings.TrimSpace(field)] = strings.TrimSpace(field)
	}

	return ips, nil
}

// AgtEthernetIpv6Addresses2 ListSutIpv6AddressesByVlan
func (p *Port) LegacyLinkGetSutIP6Addresses() ([]string, error) {
	cmd := fmt.Sprintf("AgtEthernetIpv6Addresses2 ListSutIpv6AddressesByVlan %s 0", p.Handler)
	res, err := p.Invoke(cmd)
	if err != nil {
		return nil, fmt.Errorf("Cannot get sup ip6 address : %s", err.Error())
	}

	/*If previous entries exist, drop it */
	p.LegacyLinkSutIP6 = make(map[string]string, 1)

	res = strings.Replace(res, "{", "", -1)
	res = strings.Replace(res, "}", "", -1)
	fields := strings.Split(res, " ")

	ips := make([]string, 0, 10)
	for _, field := range fields {
		ips = append(ips, strings.TrimSpace(field))
		p.LegacyLinkSutIP6[strings.TrimSpace(field)] = strings.TrimSpace(field)
	}

	return ips, nil
}

func (p *Port) LegacyLinkAddSutIPAddress(ip string) error {
	//AgtEthernetAddresses AddSutIpAddress
	cmd := fmt.Sprintf("AgtEthernetAddresses AddSutIpAddress %s %s", p.Handler, ip)
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot set Add SUT ip %s to port %s : %s", ip, p.Name, err.Error())
	}

	if p.LegacyLinkSutIP == nil {
		p.LegacyLinkSutIP = make(map[string]string, 1)
	}

	p.LegacyLinkSutIP[strings.TrimSpace(ip)] = strings.TrimSpace(ip)

	return nil
}

func (p *Port) LegacyLinkRemoveSutIPAddress(ip string) error {
	//AgtEthernetAddresses AddSutIpAddress
	cmd := fmt.Sprintf("AgtEthernetAddresses RemoveSutIpAddress %s %s", p.Handler, ip)
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot remove SUT ip %s from port %s : %s", ip, p.Name, err.Error())
	}

	if p.LegacyLinkSutIP == nil {
		p.LegacyLinkSutIP = make(map[string]string, 1)
	}

	if _, ok := p.LegacyLinkSutIP[strings.TrimSpace(ip)]; ok {
		delete(p.LegacyLinkSutIP, strings.TrimSpace(ip))
	}

	return nil
}

//In normal case we use the address pool to configure the test/sut ip addresses.
//we can create multiple address pool under one port
func (p *Port) LegacyLinkAddSutIPAddresses(start, plen, count, step string) error {
	//AgtEthernetAddresses AddSutIpAddress
	cmd := fmt.Sprintf("AgtEthernetAddresses AddSutIpAddresses %s %s %s %s %s", p.Handler, count, start, plen, step)
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot Add SUT ips to port %s : %s", p.Name, err.Error())
	}

	_, err = p.LegacyLinkGetSutIPAddresses()
	if err != nil {
		return fmt.Errorf("Cannot get SUT ips on port %s : %s", p.Name, err.Error())
	}

	return nil
}

//SUT's mac address is resoved by arp. Anyway we can also configure
func (p *Port) LegacyLinkGetSutMacAddress() (string, error) {
	cmd := fmt.Sprintf("AgtEthernetAddresses GetSutMacAddress %s", p.Handler)
	res, err := p.Invoke(cmd)
	if err != nil {
		return "", fmt.Errorf("Cannot get SUT mac for port %s : %s", p.Name, err.Error())
	}

	p.LegacyLinkSutMac = strings.TrimSpace(res)

	return strings.TrimSpace(res), nil
}

func (p *Port) LegacyLinkSetSutMacAddress(ip, mac string) (string, error) {
	cmd := fmt.Sprintf("AgtEthernetAddresses SetSutMacAddress %s %s %s", p.Handler, ip, mac)
	_, err := p.Invoke(cmd)
	if err != nil {
		return "", fmt.Errorf("Cannot set SUT mac %s for port %s : %s", mac, p.Name, err.Error())
	}

	p.LegacyLinkSutMac = strings.TrimSpace(mac)

	return strings.TrimSpace(mac), nil
}

func (p *Port) SetMediaType(media PortMediaType) error {
	if _, ok := p.MediaTypes[media]; !ok {
		return fmt.Errorf("Unsupported mediay type: %d", media)
	}
	cmd := fmt.Sprintf("AgtOpticalInterface SetMediaType %s %s", p.Handler, p.MediaTypes[media])
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot set port %s media type : %s", p.Name, err.Error())
	}
	p.MediaType = media
	return nil
}

func (p *Port) GetMediaType() (PortMediaType, error) {
	cmd := fmt.Sprintf("AgtOpticalInterface GetMediaType %s", p.Handler)
	res, err := p.Invoke(cmd)
	if err != nil {
		return 0, fmt.Errorf("Cannot set port %s media type : %s", p.Name, err.Error())
	}

	for iType, sType := range p.MediaTypes {
		if sType == strings.TrimSpace(res) {
			p.MediaType = iType
			return iType, nil
		}
	}
	return 0, fmt.Errorf("Unknown port media type :%s for port %s", res, p.Name)
}

func (p *Port) GetAvailableMediaTypes() ([]string, error) {
	cmd := fmt.Sprintf("AgtOpticalInterface ListAvailableMediaTypes %s", p.Handler)
	res, err := p.Invoke(cmd)
	if err != nil {
		return nil, fmt.Errorf("Cannot get available media types for port %s : %s", p.Name, err.Error())
	}

	res = strings.Replace(res, "{", "", -1)
	res = strings.Replace(res, "}", "", -1)
	fields := strings.Split(res, " ")

	if p.MediaTypes == nil {
		p.MediaTypes = make(map[PortMediaType]string, 2)
	}

	types := make([]string, 0, 2)

	for _, field := range fields {
		if strings.TrimSpace(field) == "AGT_MEDIA_SFP" {
			p.MediaTypes[SFP] = strings.TrimSpace(field)
		} else if strings.TrimSpace(field) == "AGT_MEDIA_RJ45" {
			p.MediaTypes[RJ45] = strings.TrimSpace(field)
		} else {
			fmt.Printf("Unknown media type: %s for port: %s", field, p.Name)
			continue
		}
		types = append(types, field)
	}
	return types, nil
}

func (p *Port) LaserOn() error {
	cmd := fmt.Sprintf("AgtOpticalInterface LaserOn %s", p.Handler)
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot set laseron for port %s : %s", p.Name, err.Error())
	}

	return nil
}

func (p *Port) LaserOff() error {
	cmd := fmt.Sprintf("AgtOpticalInterface LaserOff %s", p.Handler)
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot set laseroff for port %s : %s", p.Name, err.Error())
	}

	return nil
}

func (p *Port) SendAllArpRequests() error {
	cmd := fmt.Sprintf("AgtArpEmulation SendAllArpRequests %s", p.Handler)
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot send arp on port %s : %s", p.Name, err.Error())
	}

	return nil
}

func (p *Port) SendAllNeighborSolicitations() error {
	cmd := fmt.Sprintf("AgtArpEmulation SendAllNeighborSolicitations %s", p.Handler)
	_, err := p.Invoke(cmd)
	if err != nil {
		return fmt.Errorf("Cannot send ns on port %s : %s", p.Name, err.Error())
	}

	return nil
}

func init() {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
}

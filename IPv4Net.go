package netaddr

import (
	"fmt"
	"strings"
)

/*
ParseIPv4Net parses a string into an IPv4Net type. Accepts addresses in the form of:
	* single IP (eg. 192.168.1.1 -- defaults to /32)
	* CIDR format (eg. 192.168.1.1/24)
	* extended format (eg. 192.168.1.1 255.255.255.0)
*/
func ParseIPv4Net(addr string) (*IPv4Net, error) {
	addr = strings.TrimSpace(addr)
	var m32 *Mask32

	// parse out netmask
	if strings.Contains(addr, "/") { // cidr format
		addrSplit := strings.Split(addr, "/")
		if len(addrSplit) > 2 {
			return nil, fmt.Errorf("IP address contains multiple '/' characters.")
		}
		addr = addrSplit[0]
		prefix := addrSplit[1]
		var err error
		m32, err = ParseMask32(prefix)
		if err != nil {
			return nil, err
		}
	} else if strings.Contains(addr, " ") { // extended format
		addrSplit := strings.SplitN(addr, " ", 2)
		addr = addrSplit[0]
		mask := addrSplit[1]
		var err error
		m32, err = ParseMask32(mask)
		if err != nil {
			return nil, err
		}
	}

	// parse ip
	ip, err := ParseIPv4(addr)
	if err != nil {
		return nil, err
	}

	return initIPv4Net(ip, m32), nil
}

// NewIPv4Net creates a IPv4Net type from a IPv4 and Mask32.
// If m32 is nil then default to /32.
func NewIPv4Net(ip *IPv4, m32 *Mask32) (*IPv4Net, error) {
	if ip == nil {
		return nil, fmt.Errorf("Argument ip must not be nil.")
	}
	return initIPv4Net(ip, m32), nil
}

// IPv4Net represents an IPv4 network.
type IPv4Net struct {
	base *IPv4
	m32  *Mask32
}

/*
Cmp compares equality with another IPv4Net. Return:
	* 1 if this IPv4Net is numerically greater
	* 0 if the two are equal
	* -1 if this IPv4Net is numerically less

The comparasin is initially performed on using the Cmp() method of the network address,
however, in cases where the network addresses are identical then the netmasks will
be compared with the Cmp() method of the netmask.
*/
func (net *IPv4Net) Cmp(other *IPv4Net) (int, error) {
	if other == nil {
		return 0, fmt.Errorf("Argument other must not be nil.")
	}

	res, err := net.base.Cmp(other.base)
	if err != nil {
		return 0, err
	} else if res != 0 {
		return res, nil
	}

	return net.m32.Cmp(other.m32), nil
}

// Extended returns the network address as a string in extended format.
func (net *IPv4Net) Extended() string {
	return net.base.String() + " " + net.m32.Extended()
}

// Fill returns a copy of the given IPv4NetList, stripped of
// any networks which are not subnets of this IPv4Net, and
// with any missing gaps filled in.
func (net *IPv4Net) Fill(list IPv4NetList) IPv4NetList {
	var subs IPv4NetList
	// get rid of non subnets
	if len(list) > 0 {
		for _, e := range list {
			isRel, rel := net.Rel(e)
			if isRel && rel == 1 { // e is a subnet
				subs = append(subs, e)
			}
		}
		// summarize & sort
		subs = subs.Summ()
	} else {
		return subs
	}

	// fill
	var filled IPv4NetList
	if len(subs) > 0 {
		// bottom fill if base address is missing
		base := net.base.addr
		if subs[0].base.addr != base {
			filled = subs[0].backfill(base)
		}

		// fill gaps between subnets
		sib := net.nthSib(1, false)
		var ceil uint32
		if sib != nil {
			ceil = sib.base.addr
		} else {
			ceil = ALL_ONES32
		}
		for i := 0; i < len(subs); i += 1 {
			sub := subs[i]
			filled = append(filled, sub)
			// we need to define a limit for this round
			var limit uint32
			if i+1 < len(subs) {
				limit = subs[i+1].base.addr
			} else {
				limit = ceil
			}
			filled = append(filled, sub.fwdFill(limit)...)
		}
	}
	return filled
}

/*
IPs returns IP addresses belonging to this IPv4Net.
The arguments are as follows:
	* page -- the set to return. starts with page 0.
	* perPage -- the max number of addresses to return. defaults to 32.
*/
func (net *IPv4Net) IPs(page, perPage uint32) (IPv4List, error) {
	ipCount := net.m32.Len()
	nth := page * perPage
	if nth > ipCount-1 {
		return nil, fmt.Errorf("Maximum of %d addresses available. Page %d, PerPage %d exceeds limit.", ipCount, page, perPage)
	}

	// set default or limit to ipCount
	if perPage == 0 {
		if ipCount > 32 {
			perPage = 32
		} else {
			perPage = ipCount
		}
	} else if perPage > ipCount {
		perPage = ipCount
	}

	list := make(IPv4List, perPage, perPage)
	var i uint32
	for ; i < perPage; i += 1 {
		list[i] = NewIPv4(net.base.addr + nth)
		nth += 1
	}
	return list, nil
}

// Len returns the number of IP addresses in this network.
// It will always return 0 for /0 networks.
func (net *IPv4Net) Len() uint32 {
	return net.m32.Len()
}

// Netmask returns the Mask32 used by the IPv4Net.
func (net *IPv4Net) Netmask() *Mask32 {
	return net.m32
}

// Network returns the network address of the IPv4Net.
func (net *IPv4Net) Network() *IPv4 {
	return net.base
}

// Next returns the next largest consecutive IP network
// or nil if the end of the address space is reached.
func (net *IPv4Net) Next() *IPv4Net {
	addr := net.nthSib(1, false)
	if addr == nil { // end of address space reached
		return nil
	}
	return addr.grow()
}

// NextSib returns the network immediately following this one.
// It will return nil if the end of the address space is reached.
func (net *IPv4Net) NextSib() *IPv4Net {
	addr := net.nthSib(1, false)
	if addr == nil { // end of address space reached
		return nil
	}
	return addr
}

// Nth returns the IP address at the given index.
// The size of the network may be determined with the Len() method.
// If the range is exceeded then return nil.
func (net *IPv4Net) Nth(index uint32) *IPv4 {
	if index >= net.Len() {
		return nil
	}
	return NewIPv4(net.base.addr + index)
}

// Prev returns the previous largest consecutive IP network
// or nil if the start of the address space is reached.
func (net *IPv4Net) Prev() *IPv4Net {
	if net.base.addr == 0 { // start of address space reached
		return nil
	}
	resized := net.grow()
	return resized.nthSib(1, true)
}

// PrevSib returns the network immediately preceding this one.
// It will return nil if this is 0.0.0.0.
func (net *IPv4Net) PrevSib() *IPv4Net {
	if net.base.addr == 0 { // start of address space reached
		return nil
	}
	return net.nthSib(1, true)
}

/*
Rel determines the relationship to another IPv4Net. The method returns
two values: a bool and an int. If the bool is false, then the two networks
are unrelated and the int will be 0. If the bool is true, then the int will
be interpreted as:
	* 1 if this IPv4Net is the supernet of other
	* 0 if the two are equal
	* -1 if this IPv4Net is a subnet of other
*/
func (net *IPv4Net) Rel(other *IPv4Net) (bool, int) {
	if other == nil {
		return false, 0
	}

	// when networks are equal then we can look exlusively at the netmask
	if net.base.addr == other.base.addr {
		return true, net.m32.Cmp(other.m32)
	}

	// when networks are not equal we can use hostmask to test if they are
	// related and which is the supernet vs the subnet
	netHostmask := net.m32.mask ^ ALL_ONES32
	otherHostmask := other.m32.mask ^ ALL_ONES32
	if net.base.addr|netHostmask == other.base.addr|netHostmask {
		return true, 1
	} else if net.base.addr|otherHostmask == other.base.addr|otherHostmask {
		return true, -1
	}
	return false, 0
}

// Resize returns a copy of the network with an adjusted netmask.
func (net *IPv4Net) Resize(prefix uint) (*IPv4Net, error) {
	m32, err := NewMask32(prefix)
	if err != nil {
		return nil, err
	}
	return NewIPv4Net(net.base, m32)
}

// String returns the network address as a string in CIDR format.
func (net *IPv4Net) String() string {
	return net.base.String() + net.m32.String()
}

/*
Subnet creates and returns subnets of this IPv4Net.
The arguments are as follows:
	* prefix -- the prefix length of the new subnets. must be longer than prefix of this IPv4Net.
	* page -- the set to return. starts with page 0.
	* perPage -- the max number of subnets to return. defaults to 32.
*/
func (net *IPv4Net) Subnet(prefix uint, page, perPage uint32) (IPv4NetList, error) {
	if prefix <= net.m32.prefix {
		return nil, fmt.Errorf("Prefix length must be greater than /%d.", net.m32.prefix)
	}
	m32, err := NewMask32(prefix)
	if err != nil {
		return nil, err
	}
	var maxSubs uint32 = 1 << (prefix - net.m32.prefix) // maxium number of subnets
	nth := page * perPage
	if nth > maxSubs-1 {
		return nil, fmt.Errorf("Maximum of %d subnets available. Page %d, PerPage %d exceeds limit.", maxSubs, page, perPage)
	}

	// set default or limit to maxSubs
	if perPage == 0 {
		if maxSubs > 32 {
			perPage = 32
		} else {
			perPage = maxSubs
		}
	} else if perPage > maxSubs {
		perPage = maxSubs
	}

	subBase, _ := NewIPv4Net(net.base, m32)
	list := make(IPv4NetList, perPage, perPage)
	if nth != 0 {
		subBase = subBase.nthSib(nth, false)
	}
	list[0] = subBase
	nth = 1
	for ; nth < perPage; nth += 1 {
		sub := subBase.nthSib(nth, false)
		list[nth] = sub
	}
	return list, nil
}

// SubnetCount returns the number a subnets of a given prefix length that this IPv4Net contains.
// It will return 0 for invalid requests (ie. bad prefix or prefix is shorter than that of this network).
// It will also return 0 if the result exceeds the capacity of uint32 (ie. if you want the # of /32 a /0 will hold)
func (net *IPv4Net) SubnetCount(prefix uint) uint32 {
	if prefix <= net.m32.prefix || prefix > 32 {
		return 0
	}
	return 1 << (prefix - net.m32.prefix)
}

// Summ creates a summary address from this IPv4Net and another.
// It errors if the two networks are incapable of being summarized.
func (net *IPv4Net) Summ(other *IPv4Net) (*IPv4Net, error) {
	if other == nil {
		return nil, fmt.Errorf("Argument other must not be nil.")
	}
	if net.m32.prefix != other.m32.prefix {
		return nil, fmt.Errorf("%s and %s have mismatched prefix lengths.", net, other)
	}

	// merge-able networks will be identical if you right shift them
	// by the number of bits in the hostmask + 1.
	shift := 32 - net.m32.prefix + 1
	addr := net.base.addr >> shift
	otherAddr := other.base.addr >> shift
	if addr != otherAddr {
		return nil, fmt.Errorf("%s and %s do not fall within a common bit boundary.", net, other)
	}
	return net.Resize(net.m32.prefix - 1)
}

// NON EXPORTED

// backfill generates subnets between this net and the limit address.
// limit should be < net. will create subnets up to and including limit.
func (net *IPv4Net) backfill(limit uint32) IPv4NetList {
	var nets IPv4NetList
	cur := net
	for {
		prev := cur.Prev()
		if prev == nil || prev.base.addr < limit {
			break
		}
		nets = append(IPv4NetList{prev}, nets...)
		cur = prev
	}
	return nets
}

// fwdFill returns subnets between this net and the limit address.
// limit should be > net. will create subnets up to limit.
func (net *IPv4Net) fwdFill(limit uint32) IPv4NetList {
	var nets IPv4NetList
	cur := net
	for {
		next := cur.Next()
		if next == nil || next.base.addr >= limit {
			break
		}
		nets = append(nets, next)
		cur = next
	}
	return nets
}

// initIPv4Net initializes a new IPv4Net
func initIPv4Net(ip *IPv4, m32 *Mask32) *IPv4Net {
	net := new(IPv4Net)
	if m32 == nil {
		m32 = initMask32(32)
		net.m32 = m32
	} else {
		m32 = m32.dup()
	}
	net.m32 = m32
	net.base = NewIPv4(ip.addr & m32.mask) // set base ip
	return net
}

// grow decreases the netmask as much as possible without crossing a bit boundary.
func (net *IPv4Net) grow() *IPv4Net {
	addr := net.base.addr
	mask := net.m32.mask
	var prefix uint
	for prefix = net.m32.prefix; prefix >= 0; prefix -= 1 {
		mask = mask << 1
		if addr|mask != mask || prefix == 0 { // bit boundary crossed when there are '1' bits in the host portion
			break
		}
	}
	return initIPv4Net(NewIPv4(addr), initMask32(prefix))
}

// nthSib returns the nth next sibling network or nil if address space exceeded.
// nthSib will return the nth previous sibling if prev is true
func (net *IPv4Net) nthSib(nth uint32, prev bool) *IPv4Net {
	var addr uint32
	// right shift by # of bits of host portion of address, add nth.
	// and left shift back. this is the sibling network.
	shift := 32 - net.m32.prefix
	if prev {
		addr = (net.base.addr>>shift - nth) << shift
		if addr > net.base.addr {
			return nil
		}
	} else {
		addr = (net.base.addr>>shift + nth) << shift
		if addr < net.base.addr {
			return nil
		}
	}
	return initIPv4Net(NewIPv4(addr), net.m32)
}
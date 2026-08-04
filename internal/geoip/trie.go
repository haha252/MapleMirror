package geoip

import (
	"fmt"
	"net/netip"
)

type prefixTree struct {
	root *prefixNode
}

type prefixNode struct {
	region Region
	left   *prefixNode
	right  *prefixNode
}

func (t *prefixTree) insert(prefix netip.Prefix, region Region) error {
	if t.root == nil {
		t.root = &prefixNode{}
	}
	node := t.root
	addr := prefix.Addr()
	for i := 0; i < prefix.Bits(); i++ {
		if addrBit(addr, i) == 0 {
			if node.left == nil {
				node.left = &prefixNode{}
			}
			node = node.left
		} else {
			if node.right == nil {
				node.right = &prefixNode{}
			}
			node = node.right
		}
	}
	if node.region != "" && node.region != region {
		return fmt.Errorf("前缀 %s 同时属于多个地区", prefix)
	}
	node.region = region
	return nil
}

func (t prefixTree) lookup(addr netip.Addr) Region {
	if t.root == nil {
		return RegionUnknown
	}
	node := t.root
	matched := RegionUnknown
	if node.region != "" {
		matched = node.region
	}
	for i := 0; i < addr.BitLen(); i++ {
		if addrBit(addr, i) == 0 {
			node = node.left
		} else {
			node = node.right
		}
		if node == nil {
			break
		}
		if node.region != "" {
			matched = node.region
		}
	}
	return matched
}

func addrBit(addr netip.Addr, index int) uint8 {
	var data []byte
	if addr.Is4() {
		value := addr.As4()
		data = value[:]
	} else {
		value := addr.As16()
		data = value[:]
	}
	return (data[index/8] >> uint(7-index%8)) & 1
}

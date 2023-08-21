package controller

import (
	"encoding/binary"
	//"fmt"
	"math"
	"net"
	"strconv"

	"github.com/pkg/errors"
)

func GenSubnetsInNetwork(netCIDR string, subnetMaskSize int) ([]string, error) {
	ip, ipNet, err := net.ParseCIDR(netCIDR)
	if err != nil {
		return nil, err
	}
	if !ip.Equal(ipNet.IP) {
		return nil, errors.New("netCIDR is not a valid network address")
	}
	netMaskSize, _ := ipNet.Mask.Size()
	if netMaskSize > int(subnetMaskSize) {
		return nil, errors.New("subnetMaskSize must be greater or equal than netMaskSize")
	}

	totalSubnetsInNetwork := math.Pow(2, float64(subnetMaskSize)-float64(netMaskSize))
	totalHostsInSubnet := math.Pow(2, 32-float64(subnetMaskSize))
	subnetIntAddresses := make([]uint32, int(totalSubnetsInNetwork))
	// first subnet address is same as the network address
	subnetIntAddresses[0] = ip2int(ip.To4())
	for i := 1; i < int(totalSubnetsInNetwork); i++ {
		subnetIntAddresses[i] = subnetIntAddresses[i-1] + uint32(totalHostsInSubnet)
	}

	subnetCIDRs := make([]string, 0)
	for _, sia := range subnetIntAddresses {
		subnetCIDRs = append(
			subnetCIDRs,
			int2ip(sia).String()+"/"+strconv.Itoa(int(subnetMaskSize)),
		)
	}
	return subnetCIDRs, nil
}

func ip2int(ip net.IP) uint32 {
	if len(ip) == 16 {
		panic("cannot convert IPv6 into uint32")
	}
	return binary.BigEndian.Uint32(ip)
}
func int2ip(nn uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, nn)
	return ip
}

// FOR TESTING
func gkopNewGetSubnets(sn string) []string {
	initialCidrs, _ := GenSubnetsInNetwork(sn, 26)

	transit, _ := GenSubnetsInNetwork(initialCidrs[7], 28)

	//fmt.Println(initialCidrs)
	//fmt.Println(transit)

	finalCidrs := make([]string, 9)
	count := 0
	for i := 0; i < 7; i++ {
		if i != 3 {
			finalCidrs[count] = initialCidrs[i]
			count++
		}
	}
	for i := 0; i < 3; i++ {
		finalCidrs[count] = transit[i]
		count++
	}

	return finalCidrs

}

func gkopLegacyGetSubnets(sn string) []string {
	initialCidrs, _ := GenSubnetsInNetwork("10.0.0.0/23", 27)
	var public [3]string
	for i := 0; i < 3; i++ {
		public[i] = initialCidrs[i]
	}
	count := 0
	var private [3]string
	for i := 8; i < 11; i++ {
		private[count] = initialCidrs[i]
		count++
	}
	var service [3]string
	count = 0
	for i := 11; i < 14; i++ {
		service[count] = initialCidrs[i]
		count++
	}
	transit1, _ := GenSubnetsInNetwork(initialCidrs[14], 28)
	transit2, _ := GenSubnetsInNetwork(initialCidrs[15], 28)

	// Merge all subnet types and return
	finalCidrs := make([]string, 12)
	for i := 0; i < 12; i++ {
		if i >= 0 && i < 3 {
			finalCidrs[i] = public[i]
		}
		if i >= 3 && i < 6 {
			count = i - 3
			finalCidrs[i] = private[count]
		}
		if i >= 6 && i < 9 {
			count = i - 6
			finalCidrs[i] = service[count]
		}
		if i >= 9 && i < 11 {
			count = i - 9
			finalCidrs[i] = transit1[count]
		}
		if i == 11 {
			finalCidrs[i] = transit2[0]
		}

	}
	return finalCidrs
}

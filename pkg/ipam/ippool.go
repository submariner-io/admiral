/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ipam

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"sync"

	"github.com/emirpasic/gods/maps/treemap"
	"github.com/emirpasic/gods/utils"
	"github.com/pkg/errors"
)

type IPPool struct {
	cidr      string
	network   *net.IPNet
	size      uint32
	available *treemap.Map // int IP is the key, string IP is the value
	mutex     sync.RWMutex
	metrics   MetricsReporter
}

type MetricsReporter interface {
	RecordAvailability(cidr string, count int)
	RecordIPsAllocated(cidr string, count int)
	RecordIPDeallocated(cidr string)
}
type MetricsReporterFuncs struct {
	RecordAvailabilityFunc  func(cidr string, count int)
	RecordIPsAllocatedFunc  func(cidr string, count int)
	RecordIPDeallocatedFunc func(cidr string)
}

func (r MetricsReporterFuncs) RecordAvailability(cidr string, count int) {
	if r.RecordAvailabilityFunc != nil {
		r.RecordAvailabilityFunc(cidr, count)
	}
}

func (r MetricsReporterFuncs) RecordIPsAllocated(cidr string, count int) {
	if r.RecordIPsAllocatedFunc != nil {
		r.RecordIPsAllocatedFunc(cidr, count)
	}
}

func (r MetricsReporterFuncs) RecordIPDeallocated(cidr string) {
	if r.RecordIPDeallocatedFunc != nil {
		r.RecordIPDeallocatedFunc(cidr)
	}
}

func NewIPPool(cidr string, metrics MetricsReporter) (*IPPool, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, errors.Wrapf(err, "error parsing CIDR %q", cidr)
	}

	ones, totalbits := network.Mask.Size()

	size := uint32(math.Exp2(float64(totalbits - ones)))
	if size < 4 {
		return nil, fmt.Errorf("invalid prefix for CIDR %q", cidr)
	}

	size -= 2 // don't count net and broadcast

	if metrics == nil {
		metrics = MetricsReporterFuncs{}
	}

	pool := &IPPool{
		cidr:      cidr,
		network:   network,
		size:      size,
		available: treemap.NewWith(utils.UInt32Comparator),
		metrics:   metrics,
	}

	startingIP := ipToInt(pool.network.IP) + 1

	for i := uint32(0); i < pool.size; i++ {
		intIP := startingIP + i
		ip := intToIP(intIP).String()
		pool.available.Put(intIP, ip)
	}

	metrics.RecordAvailability(cidr, int(size))

	return pool, nil
}

func ipToInt(ip net.IP) uint32 {
	intIP := ip
	if len(ip) == 16 {
		intIP = ip[12:16]
	}

	return binary.BigEndian.Uint32(intIP)
}

func intToIP(ip uint32) net.IP {
	netIP := make(net.IP, 4)
	binary.BigEndian.PutUint32(netIP, ip)

	return netIP
}

func StringIPToInt(stringIP string) uint32 {
	return ipToInt(net.ParseIP(stringIP))
}

func (p *IPPool) allocateOne() ([]string, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	iter := p.available.Iterator()
	if iter.Last() {
		p.available.Remove(iter.Key())
		p.metrics.RecordIPsAllocated(p.cidr, 1)

		return []string{iter.Value().(string)}, nil
	}

	return nil, errors.New("insufficient IPs available for allocation")
}

func (p *IPPool) Allocate(num int) ([]string, error) {
	switch {
	case num < 0:
		return nil, errors.New("the number to allocate cannot be negative")
	case num == 0:
		return []string{}, nil
	case num == 1:
		return p.allocateOne()
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.available.Size() < num {
		return nil, fmt.Errorf("insufficient IPs available (%d) to allocate %d", p.available.Size(), num)
	}

	retIPs := make([]string, num)

	var prevIntIP, firstIntIP, current uint32

	iter := p.available.Iterator()
	for iter.Next() {
		intIP := iter.Key().(uint32)
		retIPs[current] = iter.Value().(string)

		if current == 0 || prevIntIP+1 != intIP {
			firstIntIP = intIP
			prevIntIP = intIP
			retIPs[0] = retIPs[current]
			current = 1

			continue
		}

		prevIntIP = intIP
		current++

		if int(current) == num {
			for i := 0; i < num; i++ {
				p.available.Remove(firstIntIP)

				firstIntIP++
			}

			p.metrics.RecordIPsAllocated(p.cidr, num)

			return retIPs, nil
		}
	}

	return nil, fmt.Errorf("unable to allocate a contiguous block of %d IPs - available pool size is %d",
		num, p.available.Size())
}

func (p *IPPool) Release(ips ...string) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	for _, ip := range ips {
		if !p.network.Contains(net.ParseIP(ip)) {
			return fmt.Errorf("released IP %s is not contained in CIDR %s", ip, p.cidr)
		}

		p.available.Put(StringIPToInt(ip), ip)
		p.metrics.RecordIPDeallocated(p.cidr)
	}

	return nil
}

func (p *IPPool) Reserve(ips ...string) error {
	num := len(ips)
	if num == 0 {
		return nil
	}

	intIPs := make([]uint32, num)

	p.mutex.Lock()
	defer p.mutex.Unlock()

	for i := 0; i < num; i++ {
		intIPs[i] = StringIPToInt(ips[i])

		_, found := p.available.Get(intIPs[i])
		if !found {
			if !p.network.Contains(net.ParseIP(ips[i])) {
				return fmt.Errorf("the requested IP %s is not contained in CIDR %s", ips[i], p.cidr)
			}

			return fmt.Errorf("the requested IP %s is already allocated", ips[i])
		}
	}

	for i := 0; i < num; i++ {
		p.available.Remove(intIPs[i])
	}

	p.metrics.RecordIPsAllocated(p.cidr, num)

	return nil
}

func (p *IPPool) Size() int {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return p.available.Size()
}

func (p *IPPool) GetCIDR() string {
	return p.cidr
}

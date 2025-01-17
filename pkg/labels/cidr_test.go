// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package labels

import (
	"math/rand"
	"net/netip"
	"runtime"
	"strconv"
	"sync"
	"testing"

	. "github.com/cilium/checkmate"
	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/stretchr/testify/assert"

	"github.com/cilium/cilium/pkg/checker"
	"github.com/cilium/cilium/pkg/option"
)

// TestGetCIDRLabels checks that GetCIDRLabels returns a sane set of labels for
// given CIDRs.
func (s *LabelsSuite) TestGetCIDRLabels(c *C) {
	option.Config.EnableIPv6 = false
	prefix := netip.MustParsePrefix("192.0.2.3/32")
	expected := ParseLabelArray(
		"cidr:0.0.0.0/0",
		"cidr:128.0.0.0/1",
		"cidr:192.0.0.0/8",
		"cidr:192.0.2.0/24",
		"cidr:192.0.2.3/32",
		"reserved:world",
	)

	lbls := GetCIDRLabels(prefix)
	lblArray := lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
	// IPs should be masked as the labels are generated
	c.Assert(lblArray.Has("cidr:192.0.2.3/24"), Equals, false)

	prefix = netip.MustParsePrefix("192.0.2.0/24")
	expected = ParseLabelArray(
		"cidr:0.0.0.0/0",
		"cidr:192.0.2.0/24",
		"reserved:world",
	)

	lbls = GetCIDRLabels(prefix)
	lblArray = lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
	// CIDRs that are covered by the prefix should not be in the labels
	c.Assert(lblArray.Has("cidr.192.0.2.3/32"), Equals, false)

	// Zero-length prefix / default route should become reserved:world.
	prefix = netip.MustParsePrefix("0.0.0.0/0")
	expected = ParseLabelArray(
		"reserved:world",
	)

	lbls = GetCIDRLabels(prefix)
	lblArray = lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
	c.Assert(lblArray.Has("cidr.0.0.0.0/0"), Equals, false)

	// Note that we convert the colons in IPv6 addresses into dashes when
	// translating into labels, because endpointSelectors don't support
	// colons.
	option.Config.EnableIPv6 = true
	option.Config.EnableIPv4 = false
	prefix = netip.MustParsePrefix("2001:DB8::1/128")
	expected = ParseLabelArray(
		"cidr:0--0/0",
		"cidr:2000--0/3",
		"cidr:2001--0/16",
		"cidr:2001-d00--0/24",
		"cidr:2001-db8--0/32",
		"cidr:2001-db8--1/128",
		"reserved:world",
	)

	lbls = GetCIDRLabels(prefix)
	lblArray = lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
	// IPs should be masked as the labels are generated
	c.Assert(lblArray.Has("cidr.2001-db8--1/24"), Equals, false)
	option.Config.EnableIPv4 = true
}

// TestGetCIDRLabelsDualStack checks that GetCIDRLabels returns a sane set of labels for
// given CIDRs in dual stack mode.
func (s *LabelsSuite) TestGetCIDRLabelsDualStack(c *C) {
	prefix := netip.MustParsePrefix("192.0.2.3/32")
	expected := ParseLabelArray(
		"cidr:0.0.0.0/0",
		"cidr:128.0.0.0/1",
		"cidr:192.0.0.0/8",
		"cidr:192.0.2.0/24",
		"cidr:192.0.2.3/32",
		"reserved:world-ipv4",
	)

	lbls := GetCIDRLabels(prefix)
	lblArray := lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
	// IPs should be masked as the labels are generated
	c.Assert(lblArray.Has("cidr:192.0.2.3/24"), Equals, false)

	prefix = netip.MustParsePrefix("192.0.2.0/24")
	expected = ParseLabelArray(
		"cidr:0.0.0.0/0",
		"cidr:192.0.2.0/24",
		"reserved:world-ipv4",
	)

	lbls = GetCIDRLabels(prefix)
	lblArray = lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
	// CIDRs that are covered by the prefix should not be in the labels
	c.Assert(lblArray.Has("cidr.192.0.2.3/32"), Equals, false)

	// Zero-length prefix / default route should become reserved:world.
	prefix = netip.MustParsePrefix("0.0.0.0/0")
	expected = ParseLabelArray(
		"reserved:world-ipv4",
	)

	lbls = GetCIDRLabels(prefix)
	lblArray = lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
	c.Assert(lblArray.Has("cidr.0.0.0.0/0"), Equals, false)

	// Note that we convert the colons in IPv6 addresses into dashes when
	// translating into labels, because endpointSelectors don't support
	// colons.
	prefix = netip.MustParsePrefix("2001:DB8::1/128")
	expected = ParseLabelArray(
		"cidr:0--0/0",
		"cidr:2000--0/3",
		"cidr:2001--0/16",
		"cidr:2001-d00--0/24",
		"cidr:2001-db8--0/32",
		"cidr:2001-db8--1/128",
		"reserved:world-ipv6",
	)

	lbls = GetCIDRLabels(prefix)
	lblArray = lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
	// IPs should be masked as the labels are generated
	c.Assert(lblArray.Has("cidr.2001-db8--1/24"), Equals, false)
}

// TestGetCIDRLabelsInCluster checks that the cluster label is properly added
// when getting labels for CIDRs that are equal to or within the cluster range.
func (s *LabelsSuite) TestGetCIDRLabelsInCluster(c *C) {
	option.Config.EnableIPv6 = false
	prefix := netip.MustParsePrefix("10.0.0.0/16")
	expected := ParseLabelArray(
		"cidr:0.0.0.0/0",
		"cidr:10.0.0.0/16",
		"reserved:world",
	)
	lbls := GetCIDRLabels(prefix)
	lblArray := lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})

	option.Config.EnableIPv6 = true
	option.Config.EnableIPv4 = false
	// This case is firmly within the cluster range
	prefix = netip.MustParsePrefix("2001:db8:cafe::cab:4:b0b:0/112")
	expected = ParseLabelArray(
		"cidr:0--0/0",
		"cidr:2001-db8-cafe--0/64",
		"cidr:2001-db8-cafe-0-cab-4--0/96",
		"cidr:2001-db8-cafe-0-cab-4-b0b-0/112",
		"reserved:world",
	)
	lbls = GetCIDRLabels(prefix)
	lblArray = lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
	option.Config.EnableIPv4 = true
}

// TestGetCIDRLabelsInClusterDualStack checks that the cluster label is properly added
// when getting labels for CIDRs that are equal to or within the cluster range in dual
// stack mode.
func (s *LabelsSuite) TestGetCIDRLabelsInClusterDualStack(c *C) {
	prefix := netip.MustParsePrefix("10.0.0.0/16")
	expected := ParseLabelArray(
		"cidr:0.0.0.0/0",
		"cidr:10.0.0.0/16",
		"reserved:world-ipv4",
	)
	lbls := GetCIDRLabels(prefix)
	lblArray := lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})

	// This case is firmly within the cluster range
	prefix = netip.MustParsePrefix("2001:db8:cafe::cab:4:b0b:0/112")
	expected = ParseLabelArray(
		"cidr:0--0/0",
		"cidr:2001-db8-cafe--0/64",
		"cidr:2001-db8-cafe-0-cab-4--0/96",
		"cidr:2001-db8-cafe-0-cab-4-b0b-0/112",
		"reserved:world-ipv6",
	)
	lbls = GetCIDRLabels(prefix)
	lblArray = lbls.LabelArray()
	c.Assert(lblArray.Lacks(expected), checker.DeepEquals, LabelArray{})
}

func (s *LabelsSuite) TestIPStringToLabel(c *C) {
	for _, tc := range []struct {
		ip      string
		label   string
		wantErr bool
	}{
		{
			ip:    "0.0.0.0/0",
			label: "cidr:0.0.0.0/0",
		},
		{
			ip:    "192.0.2.3",
			label: "cidr:192.0.2.3/32",
		},
		{
			ip:    "192.0.2.3/32",
			label: "cidr:192.0.2.3/32",
		},
		{
			ip:    "192.0.2.3/24",
			label: "cidr:192.0.2.0/24",
		},
		{
			ip:    "192.0.2.0/24",
			label: "cidr:192.0.2.0/24",
		},
		{
			ip:    "::/0",
			label: "cidr:0--0/0",
		},
		{
			ip:    "fdff::ff",
			label: "cidr:fdff--ff/128",
		},
		{
			ip:    "f00d:42::ff/128",
			label: "cidr:f00d-42--ff/128",
		},
		{
			ip:    "f00d:42::ff/96",
			label: "cidr:f00d-42--0/96",
		},
		{
			ip:      "",
			wantErr: true,
		},
		{
			ip:      "foobar",
			wantErr: true,
		},
	} {
		lbl, err := IPStringToLabel(tc.ip)
		if !tc.wantErr {
			c.Assert(err, IsNil)
			c.Assert(lbl.String(), checker.DeepEquals, tc.label)
		} else {
			c.Assert(err, Not(IsNil))
		}
	}
}

func BenchmarkGetCIDRLabels(b *testing.B) {
	// clear the cache
	cidrLabelsCache, _ = simplelru.NewLRU[netip.Prefix, []Label](cidrLabelsCacheMaxSize, nil)

	for _, cidr := range []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/0"),
		netip.MustParsePrefix("10.16.0.0/16"),
		netip.MustParsePrefix("192.0.2.3/32"),
		netip.MustParsePrefix("192.0.2.3/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("::/0"),
		netip.MustParsePrefix("fdff::ff/128"),
		netip.MustParsePrefix("f00d:42::ff/128"),
		netip.MustParsePrefix("f00d:42::ff/96"),
	} {
		b.Run(cidr.String(), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = GetCIDRLabels(cidr)
			}
		})
	}
}

// This benchmarks SortedList(). We want to benchmark this specific case, as
// it is excercised by toFQDN policies.
func BenchmarkLabels_SortedListCIDRIDs(b *testing.B) {
	// clear the cache
	cidrLabelsCache, _ = simplelru.NewLRU[netip.Prefix, []Label](cidrLabelsCacheMaxSize, nil)

	lbls := GetCIDRLabels(netip.MustParsePrefix("123.123.123.123/32"))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lbls.SortedList()
	}
}

func BenchmarkGetCIDRLabelsConcurrent(b *testing.B) {
	prefixes := make([]netip.Prefix, 0, 16)
	octets := [4]byte{0, 0, 1, 1}
	for i := 0; i < 16; i++ {
		octets[0], octets[1] = byte(rand.Intn(256)), byte(rand.Intn(256))
		prefixes = append(prefixes, netip.PrefixFrom(netip.AddrFrom4(octets), 32))
	}

	for _, goroutines := range []int{1, 2, 4, 16, 32, 48} {
		b.Run(strconv.Itoa(goroutines), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				start := make(chan struct{})
				var wg sync.WaitGroup

				wg.Add(goroutines)
				for j := 0; j < goroutines; j++ {
					go func() {
						defer wg.Done()

						<-start

						for k := 0; k < 64; k++ {
							_ = GetCIDRLabels(prefixes[rand.Intn(len(prefixes))])
						}
					}()
				}

				b.StartTimer()
				close(start)
				wg.Wait()
			}
		})
	}
}

// BenchmarkCIDRLabelsCacheHeapUsageIPv4 should be run with -benchtime=1x
func BenchmarkCIDRLabelsCacheHeapUsageIPv4(b *testing.B) {
	b.Skip()

	// clear the cache
	cidrLabelsCache, _ = simplelru.NewLRU[netip.Prefix, []Label](cidrLabelsCacheMaxSize, nil)

	// be sure to fill the cache
	prefixes := make([]netip.Prefix, 0, 256*256)
	octets := [4]byte{0, 0, 1, 1}
	for i := 0; i < 256*256; i++ {
		octets[0], octets[1] = byte(i/256), byte(i%256)
		prefixes = append(prefixes, netip.PrefixFrom(netip.AddrFrom4(octets), 32))
	}

	var m1, m2 runtime.MemStats
	// One GC does not give precise results,
	// because concurrent sweep may be still in progress.
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m1)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, cidr := range prefixes {
			_ = GetCIDRLabels(cidr)
		}
	}
	b.StopTimer()

	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m2)

	usage := m2.HeapAlloc - m1.HeapAlloc
	b.Logf("Memoization map heap usage: %.2f KiB", float64(usage)/1024)
}

// BenchmarkCIDRLabelsCacheHeapUsageIPv6 should be run with -benchtime=1x
func BenchmarkCIDRLabelsCacheHeapUsageIPv6(b *testing.B) {
	b.Skip()

	// clear the cache
	cidrLabelsCache, _ = simplelru.NewLRU[netip.Prefix, []Label](cidrLabelsCacheMaxSize, nil)

	// be sure to fill the cache
	prefixes := make([]netip.Prefix, 0, 256*256)
	octets := [16]byte{
		0x00, 0x00, 0x00, 0xd8, 0x33, 0x33, 0x44, 0x44,
		0x55, 0x55, 0x66, 0x66, 0x77, 0x77, 0x88, 0x88,
	}
	for i := 0; i < 256*256; i++ {
		octets[15], octets[14] = byte(i/256), byte(i%256)
		prefixes = append(prefixes, netip.PrefixFrom(netip.AddrFrom16(octets), 128))
	}

	var m1, m2 runtime.MemStats
	// One GC does not give precise results,
	// because concurrent sweep may be still in progress.
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m1)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, cidr := range prefixes {
			_ = GetCIDRLabels(cidr)
		}
	}
	b.StopTimer()

	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m2)

	usage := m2.HeapAlloc - m1.HeapAlloc
	b.Logf("Memoization map heap usage: %.2f KiB", float64(usage)/1024)
}

func BenchmarkIPStringToLabel(b *testing.B) {
	for _, ip := range []string{
		"0.0.0.0/0",
		"192.0.2.3",
		"192.0.2.3/32",
		"192.0.2.3/24",
		"192.0.2.0/24",
		"::/0",
		"fdff::ff",
		"f00d:42::ff/128",
		"f00d:42::ff/96",
	} {
		b.Run(ip, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := IPStringToLabel(ip)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestGetPrintableModel(t *testing.T) {
	assert.Equal(t,
		[]string{"k8s:foo=bar"},
		NewLabelsFromModel([]string{
			"k8s:foo=bar",
		}).GetPrintableModel(),
	)

	assert.Equal(t,
		[]string{
			"k8s:foo=bar",
			"reserved:remote-node",
		},
		NewLabelsFromModel([]string{
			"k8s:foo=bar",
			"reserved:remote-node",
		}).GetPrintableModel(),
	)

	assert.Equal(t,
		[]string{
			"k8s:foo=bar",
			"reserved:remote-node",
		},
		NewLabelsFromModel([]string{
			"k8s:foo=bar",
			"reserved:remote-node",
		}).GetPrintableModel(),
	)

	// Test multiple CIDRs, as well as other labels
	cl := NewLabelsFromModel([]string{
		"k8s:foo=bar",
		"reserved:remote-node",
	})
	cl.MergeLabels(GetCIDRLabels(netip.MustParsePrefix("10.0.0.6/32")))
	cl.MergeLabels(GetCIDRLabels(netip.MustParsePrefix("10.0.1.0/24")))
	cl.MergeLabels(GetCIDRLabels(netip.MustParsePrefix("192.168.0.0/24")))
	cl.MergeLabels(GetCIDRLabels(netip.MustParsePrefix("fc00:c111::5/128")))
	cl.MergeLabels(GetCIDRLabels(netip.MustParsePrefix("fc00:c112::0/64")))
	assert.Equal(t,
		[]string{
			"cidr:10.0.0.6/32",
			"cidr:10.0.1.0/24",
			"cidr:192.168.0.0/24",
			"cidr:fc00:c111::5/128",
			"cidr:fc00:c112::0/64",
			"k8s:foo=bar",
			"reserved:remote-node",
			"reserved:world-ipv4",
			"reserved:world-ipv6",
		},
		cl.GetPrintableModel(),
	)
}

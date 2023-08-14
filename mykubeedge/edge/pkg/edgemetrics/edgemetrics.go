package edgemetrics

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/shirou/gopsutil/v3/process"
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"
)

var namespace = "node"
var subsystem = "kubeedge"
var registerMetrics sync.Once

var EdgeMetrics = newEdgeMetrics()

type KubeEdgeMetrics struct {
	CloudEdgeTrafficCollector *metrics.CounterVec
	EdgeEdgePacketCollector   *metrics.CounterVec
	EdgecoreCPUPercent        *metrics.Gauge
	EdgecoreMemUsage          *metrics.Gauge
}

func newEdgeMetrics() *KubeEdgeMetrics {
	cloudEdgeTrafficCollector := metrics.NewCounterVec(
		&metrics.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "cloud_edge_metadata_traffic_collector",
			Help:      "collector of metadata traffic between cloud and edge forward by edgehub(unit: byte)",
		},
		[]string{"resource"})
	edgeEdgePakcetCollector := metrics.NewCounterVec(
		&metrics.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "edge_edge_packet_collector",
			Help:      "collector of traffic between edge node and edge nodes(unit: byte)",
		},
		[]string{"type"})
	edgecoreCPUPercent := metrics.NewGauge(
		&metrics.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "edgecore_cpu_percent",
			Help:      "percent rate of edgecore process in edge node(unit: %)",
		})
	edgecoreMemUsage := metrics.NewGauge(
		&metrics.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "edgecore_memory_usage",
			Help:      "physical memory usage of edgecore process in edge node(unit: bytes)",
		})
	registerMetrics.Do(func() {
		legacyregistry.MustRegister(cloudEdgeTrafficCollector)
		legacyregistry.MustRegister(edgeEdgePakcetCollector)
		legacyregistry.MustRegister(edgecoreCPUPercent)
		legacyregistry.MustRegister(edgecoreMemUsage)
	})
	em := &KubeEdgeMetrics{
		CloudEdgeTrafficCollector: cloudEdgeTrafficCollector,
		EdgeEdgePacketCollector:   edgeEdgePakcetCollector,
		EdgecoreCPUPercent:        edgecoreCPUPercent,
		EdgecoreMemUsage:          edgecoreMemUsage}
	//启动gopacket抓包
	go goPacket(em)
	// 监控CPU内存
	go getCpuMemInfo(em)
	return em
}

func (em *KubeEdgeMetrics) Reset() {
	em.CloudEdgeTrafficCollector.Reset()
	em.EdgeEdgePacketCollector.Reset()
	em.EdgecoreCPUPercent.Set(0)
	em.EdgecoreMemUsage.Set(0)
}

func (em *KubeEdgeMetrics) AddCloudEdgeTrafficCollector(resource string, size int) {
	if size > 0 {
		em.CloudEdgeTrafficCollector.WithLabelValues(resource).Add(float64(size))
	}
}

func (em *KubeEdgeMetrics) AddEdgeEdgePacketCollector(trafficType string, size int) {
	if size > 0 {
		em.EdgeEdgePacketCollector.WithLabelValues(trafficType).Add(float64(size))
	}
}

var (
	device      string = "enp0s3"
	snapshotLen int32  = 1024
	promiscuous bool   = false
	err         error
	timeout     time.Duration = 5 * time.Second
	handle      *pcap.Handle
)

func goPacket(em *KubeEdgeMetrics) {
	// klog.Info("gopacket start")
	// Open device
	handle, err = pcap.OpenLive(device, snapshotLen, promiscuous, timeout)
	if err != nil {
		klog.Fatal(err)
	}
	defer handle.Close()

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	for packet := range packetSource.Packets() {
		ipLayer := packet.Layer(layers.LayerTypeIPv4)
		if ipLayer != nil {
			// klog.Infoln("IPv4 layer detected.")
			ip, _ := ipLayer.(*layers.IPv4)

			// IP layer variables:
			// Version (Either 4 or 6)
			// IHL (IP Header Length in 32-bit words)
			// TOS, Length, Id, Flags, FragOffset, TTL, Protocol (TCP?),
			// Checksum, SrcIP, DstIP
			// klog.Infof("From %s to %s\n", ip.SrcIP, ip.DstIP)
			// klog.Info("Length: ", ip.Length)
			if len(ip.SrcIP.String()) == 13 && len(ip.DstIP.String()) == 13 && strings.HasPrefix(ip.SrcIP.String(), "192.168.0.1") && strings.HasPrefix(ip.DstIP.String(), "192.168.0.1") {
				// klog.Info("get edge edge packet: ", ip.Length)
				em.AddEdgeEdgePacketCollector("edge-edge", int(ip.Length))
			}
		}

		// Check for errors
		if err := packet.ErrorLayer(); err != nil {
			klog.Error("Error decoding some part of the packet:", err)
		}
	}
}

func getCpuMemInfo(em *KubeEdgeMetrics) {
	edgecore, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		klog.Error(err)
	}
	for {
		cpuPercent, err := edgecore.CPUPercent()
		if err != nil {
			klog.Error(err)
		}
		// klog.Info("get edgecore cpu percent: ", cpuPercent)
		em.EdgecoreCPUPercent.Set(cpuPercent)
		memInfo, err := edgecore.MemoryInfo()
		if err != nil {
			klog.Error(err)
		}
		// klog.Info("get edgecore mem usage: ", memInfo.RSS)
		em.EdgecoreMemUsage.Set(float64(memInfo.RSS))
		time.Sleep(1 * time.Second)
	}
}

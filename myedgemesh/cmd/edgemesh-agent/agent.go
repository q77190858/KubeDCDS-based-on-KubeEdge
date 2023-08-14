package main

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"k8s.io/component-base/logs"
	"k8s.io/klog"

	"github.com/kubeedge/edgemesh/cmd/edgemesh-agent/app"
)

var metaServerIP string

func main() {
	// go startCmdServer()
	rand.Seed(time.Now().UnixNano())
	//尝试从edgecore获取群组Leader地址，否则使用本机地址
	metaServerIP = requestMetaServerIP()
	if metaServerIP == "" {
		klog.Infof("get metaserverIP fail, use default ip")
		metaServerIP = "127.0.0.1"
	}
	ipList, err := externalIPList()
	if err != nil {
		klog.Error(err)
	}
	if strings.Contains(fmt.Sprint(ipList), metaServerIP) {
		klog.Info("metaserver is self, so connect to 127.0.0.1")
		metaServerIP = "127.0.0.1"
	}
	go checkMetaIPLoop()
	klog.Infof("edgemesh agent connecting to metaserver: %s", "http://"+metaServerIP+":10550")
	command := app.NewEdgeMeshAgentCommand("http://" + metaServerIP + ":10550")

	logs.InitLogs()
	defer logs.FlushLogs()

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
	klog.Infof("finish to start edgemesh agent")
}

func checkMetaIPLoop() {
	for {
		time.Sleep(10 * time.Second)
		result := requestMetaServerIP()
		if result != "" && result != metaServerIP {
			os.Exit(0)
		}
	}
}

func requestMetaServerIP() string {
	response, err := http.Get("http://127.0.0.1:10551/LeaderIP")
	if err != nil {
		klog.Errorf("fail to get leaderIP from 127.0.0.1:10551")
		return ""
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		klog.Errorf("fail to parse leaderIP from 127.0.0.1:10551")
		return ""
	}
	if response.StatusCode == 200 {
		metaServerIP = string(body)
		klog.Infof("get Leader IP: %s", metaServerIP)
		return metaServerIP
	}
	return ""
}

// 获取ip
func externalIPList() ([]net.IP, error) {
	ipList := []net.IP{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			ip := getIpFromAddr(addr)
			if ip == nil {
				continue
			}
			ipList = append(ipList, ip)
		}
	}
	return ipList, nil
}

// 获取ip
func getIpFromAddr(addr net.Addr) net.IP {
	var ip net.IP
	switch v := addr.(type) {
	case *net.IPNet:
		ip = v.IP
	case *net.IPAddr:
		ip = v.IP
	}
	if ip == nil || ip.IsLoopback() {
		return nil
	}
	ip = ip.To4()
	if ip == nil {
		return nil // not an ipv4 address
	}
	return ip
}

package groupmanager

import (
	"context"
	"errors"
	"io/ioutil"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/kubeedge/beehive/pkg/core"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/beehive/pkg/core/model"
	"github.com/kubeedge/kubeedge/edge/pkg/common/cloudconnection"
	"github.com/kubeedge/kubeedge/edge/pkg/common/modules"
	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/edgecore/v1alpha2"
	nodev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	// register Upgrade handler
)

//defines GroupManager object structure
type groupManager struct {
	enable        bool
	nodeName      string
	nodeIP        string
	nodeRole      string
	groupNodeList []nodev1.Node
	hasVoted      bool
	voteCount     uint32
	leaderIP      string
	lock          sync.RWMutex
	electChan     chan int
	heatbeatChan  chan int
	leaderChan    chan int
}

const (
	Follower  = "Follower"
	Candidate = "Candidate"
	Leader    = "Leader"
)

var _ core.Module = (*groupManager)(nil)

func newGroupManager(enable bool, nodeName string, nodeIP string) *groupManager {
	return &groupManager{
		enable:        enable,
		nodeName:      nodeName,
		nodeIP:        nodeIP,
		nodeRole:      Follower, //don't pesistant save
		groupNodeList: []nodev1.Node{},
		hasVoted:      false,
		voteCount:     0,
		leaderIP:      "",
		lock:          sync.RWMutex{},
		electChan:     make(chan int),
		heatbeatChan:  make(chan int),
		leaderChan:    make(chan int),
	}
}

// Register register GroupManager
func Register(gm *v1alpha2.GroupManager, nodeName string, nodeIP string) {
	core.Register(newGroupManager(gm.Enable, nodeName, nodeIP))
}

//Name returns the name of GroupManager module
func (gm *groupManager) Name() string {
	return modules.GroupManagerModuleName
}

//Group returns GroupManager group
func (gm *groupManager) Group() string {
	return modules.MetaGroup
}

//Enable indicates whether this module is enabled
func (gm *groupManager) Enable() bool {
	return gm.enable
}

//Start sets context and starts the controller
func (gm *groupManager) Start() {
	// if !strings.HasPrefix(gm.nodeIP, "192.168") {
	// 	enp0s3, err := net.InterfaceByName("enp0s3")
	// 	if err != nil {
	// 		klog.Error("fail to find node ip", err)
	// 		return
	// 	}
	// 	addrs, _ := enp0s3.Addrs()
	// 	gm.nodeIP = addrs[0].String()
	// }
	klog.Infof("this is groupmanager output info! nodeName=%s nodeIP=%s", gm.nodeName, gm.nodeIP)
	//start group server
	go gm.startGroupServer(beehiveContext.Done())
	//start raft election loop
	go gm.electLeaderLoop(gm.electChan, beehiveContext.Done())
	go gm.LeaderLoop(gm.leaderChan, beehiveContext.Done())
	time.Sleep(2 * time.Second)
	client := gm.getMetaClient()
	if client == nil {
		klog.Error("can not get metaserver client")
		return
	}
	nodeList, err := client.CoreV1().Nodes().List(context.TODO(), v1.ListOptions{})
	if err != nil {
		klog.Errorf("fail to visit metaserver, err: %+v", err)
		return
	}
	//get group's ready node list
	// klog.Infof("nodeList: %+v", nodeList)
	for _, node := range nodeList.Items {
		// klog.Infof("node: %+v", node)
		_, err := getNodeInteralIP(node)
		if isEdgeNode(node) && isReadyNode(node) && err == nil {
			gm.groupNodeList = append(gm.groupNodeList, node)
		}
	}
	//start heatbeat loop
	go gm.heartbeatLoop(gm.heatbeatChan, beehiveContext.Done())
	gm.heatbeatChan <- 1
	for {
		select {
		case <-beehiveContext.Done():
			klog.Warning("Group Manager stop")
			return
			// default:
		}
		// msg, err := beehiveContext.Receive(gm.Name())
		// if err != nil {
		// 	klog.Errorf("failed to receive msg: %v", err)
		// 	continue
		// }
		// klog.Infof("recieve msg: %+v", msg)
	}
}

func (gm *groupManager) getMetaClient() *kubernetes.Clientset {
	//connect to metaserver
	config := &rest.Config{
		Host: "http://127.0.0.1:10550",
	}
	//get client
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Info(err)
		return nil
	}
	return client
}

func (gm *groupManager) restartMetaServer(isPublic bool) {
	content := ""
	if isPublic {
		content = "restartMetaServerPublic"
	} else {
		content = "restartMetaServerPrivate"
	}
	msg := model.NewMessage("").BuildRouter(gm.Name(), modules.MetaGroup, "null", "null").FillBody(content)
	_, err := beehiveContext.SendSync(modules.MetaManagerModuleName, *msg, 10*time.Second)
	if err != nil {
		klog.Errorf("restartMetaServer failed, err: %v", err)
		return
	}
}

func (gm *groupManager) restartEdgemesh(metaServerAddr string) {
	response, err := http.Get("http://127.0.0.1:10552/Restart?ip=" + metaServerAddr)
	if err != nil {
		return
	}
	defer response.Body.Close()
	// body, err := ioutil.ReadAll(response.Body)
	// if err != nil {
	// 	return
	// }
	// if response.StatusCode == 200 && string(body) == "OK" {
	// 	klog.Infof("restart edgemesh to connect metaserver succeed: %s", metaServerAddr)
	// 	return
	// }
}

func (gm *groupManager) LeaderLoop(waitChan <-chan int, quit <-chan struct{}) {
	for {
		select {
		case <-quit:
			return
		//wait for signal to start
		case <-waitChan:
			errTimes := 0
			for {
				if !cloudconnection.IsConnected() {
					errTimes += 1
				} else {
					errTimes = 0
				}
				if errTimes >= 5 {
					gm.lock.Lock()
					gm.nodeRole = Follower
					gm.leaderIP = ""
					gm.lock.Unlock()
					gm.restartMetaServer(false)
					gm.electChan <- 1
					break
				}
				time.Sleep(10 * time.Second)
			}

		}
	}
}

func (gm *groupManager) electLeaderLoop(waitChan <-chan int, quit <-chan struct{}) {
	//if not find leader
	for {
		select {
		case <-quit:
			return
		//wait for signal to start
		case <-waitChan:
			for {
				//check leaderIP is null
				if gm.leaderIP != "" {
					gm.heatbeatChan <- 1
					break
				}
				gm.lock.Lock()
				gm.hasVoted = false
				gm.lock.Unlock()
				//ask for Leader from groupNodeList
				leaderNodeIndex := gm.askForLeaderNode()
				//if find leader
				if leaderNodeIndex != -1 {
					gm.nodeRole = Follower
					gm.leaderIP, _ = getNodeInteralIP(gm.groupNodeList[leaderNodeIndex])
					gm.heatbeatChan <- 1
					gm.restartEdgemesh(gm.leaderIP + ":10550")
					break
				}
				//random wait 1-5s
				rand.Seed(time.Now().Unix())
				time.Sleep(time.Duration(rand.Intn(4)+1) * time.Second)
				//check cloudconnect is ok
				if !cloudconnection.IsConnected() {
					continue
				}
				//set self as candidate
				gm.nodeRole = Candidate
				//vote to self
				gm.hasVoted = true
				gm.voteCount = 1
				//request other node vote
				res := gm.askForMoreHalfNodeVote()
				//if get more half votes, become Leader
				if res {
					if gm.leaderIP != "" {
						gm.heatbeatChan <- 1
						break
					}
					gm.lock.Lock()
					gm.nodeRole = Leader
					gm.leaderIP = gm.nodeIP
					gm.lock.Unlock()
					gm.sendLeaderInfo()
					gm.restartMetaServer(true)
					break
				} else {
					gm.lock.Lock()
					gm.nodeRole = Follower
					gm.lock.Unlock()
				}
			}
		}
	}
}

func (gm *groupManager) heartbeatLoop(waitChan <-chan int, quit <-chan struct{}) {
	for {
		select {
		case <-quit:
			return
		case <-waitChan:
			for {
				gm.lock.RLock()
				leaderIP := gm.leaderIP
				nodeIP := gm.nodeIP
				nodeRole := gm.nodeRole
				gm.lock.RUnlock()
				if leaderIP == "" || (leaderIP == nodeIP && nodeRole != Leader) {
					klog.Infof("leader IP is null or unreliable self, stop heatbeat and start to elect")
					gm.electChan <- 1
					break
				}
				//self node is elected as Leader
				if leaderIP == nodeIP && nodeRole == Leader {
					klog.Infof("node self is elected as Leader, stop heatbeat and start server")
					gm.sendLeaderInfo()
					gm.restartMetaServer(true)
					break
				}
				//try heartbeat 5 times
				errTimes := 0
				for i := 0; i < 5; i++ {
					response, err := http.Get("http://" + leaderIP + ":10551/nodeRole")
					if err != nil {
						errTimes += 1
						continue
					}
					defer response.Body.Close()
					body, err := ioutil.ReadAll(response.Body)
					if err != nil {
						errTimes += 1
						continue
					}
					if response.StatusCode != 200 || string(body) != Leader {
						errTimes += 1
						continue
					} else {
						break
					}
				}
				if errTimes == 5 {
					klog.Infof("leader is no response, stop heatbeat and start to elect")
					gm.electChan <- 1
					break
				}
				time.Sleep(10 * time.Second)
			}
		}
	}
}

func (gm *groupManager) askForLeaderNode() int {
	resChan := make(chan int)
	for i, node := range gm.groupNodeList {
		addr, _ := getNodeInteralIP(node)
		if addr == gm.nodeIP {
			continue
		}
		go func(index int, node nodev1.Node, resChan chan<- int) {
			response, err := http.Get("http://" + addr + ":10551/nodeRole")
			if err != nil {
				resChan <- -1
				return
			}
			defer response.Body.Close()
			body, err := ioutil.ReadAll(response.Body)
			if err != nil {
				resChan <- -1
				return
			}
			if response.StatusCode == 200 && string(body) == Leader {
				klog.Infof("find Leader node: %s", node.Name)
				resChan <- index
				return
			}
			resChan <- -1
		}(i, node, resChan)
	}
	for _, node := range gm.groupNodeList {
		addr, _ := getNodeInteralIP(node)
		if addr == gm.nodeIP {
			continue
		}
		i := <-resChan
		if i != -1 {
			return i
		}
	}
	return -1
}

func (gm *groupManager) askForMoreHalfNodeVote() bool {
	nodeNum := len(gm.groupNodeList)
	//如果有只有一个节点，那就是本节点，直接判定自己为主节点
	if nodeNum == 1 {
		return true
	}
	resChan := make(chan uint32, nodeNum-1)
	for i, node := range gm.groupNodeList {
		addr, _ := getNodeInteralIP(node)
		if addr == gm.nodeIP {
			continue
		}
		go func(index int, node nodev1.Node, resChan chan<- uint32) {
			response, err := http.Get("http://" + addr + ":10551/RequestVote")
			if err != nil {
				klog.Errorf("error RequestVote: %s", err)
				resChan <- 0
				return
			}
			defer response.Body.Close()
			body, err := ioutil.ReadAll(response.Body)
			if err != nil {
				klog.Errorf("error response.Body ReadAll: %s", err)
				resChan <- 0
				return
			}
			if response.StatusCode == 200 && string(body) == "VoteForYou" {
				klog.Infof("get a vote from node: %s", node.Name)
				resChan <- 1
				return
			}
			resChan <- 0
		}(i, node, resChan)
	}
	for i := 0; i < nodeNum-1; i++ {
		gm.voteCount += <-resChan
		if float32(gm.voteCount) > float32(nodeNum)*0.5 {
			return true
		}
	}
	klog.Infof("fail to get more half votes, get: %d total: %d\n", gm.voteCount, nodeNum)
	return false
}

func (gm *groupManager) sendLeaderInfo() {
	for _, node := range gm.groupNodeList {
		addr, _ := getNodeInteralIP(node)
		if addr == gm.nodeIP {
			continue
		}
		go func(addr string) {
			response, err := http.Get("http://" + addr + ":10551/IAmLeader")
			if err != nil {
				klog.Errorf("error IAmLeader: %s", err)
				return
			}
			defer response.Body.Close()
			body, err := ioutil.ReadAll(response.Body)
			if err != nil {
				klog.Errorf("error response.Body ReadAll: %s", err)
				return
			}
			if response.StatusCode == 200 && string(body) == "OK" {
				return
			}
		}(addr)
	}
}

func isEdgeNode(node nodev1.Node) bool {
	for label := range node.Labels {
		if label == "node-role.kubernetes.io/edge" {
			return true
		}
	}
	return false
}

func isReadyNode(node nodev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == nodev1.NodeReady && cond.Status == nodev1.ConditionTrue {
			return true
		}
	}
	return false
}

func getNodeInteralIP(node nodev1.Node) (string, error) {
	for _, addr := range node.Status.Addresses {
		if addr.Type == nodev1.NodeInternalIP {
			return addr.Address, nil
		}
	}
	return "", errors.New("can not find internal IP")
}

package metamanager

import (
	"time"

	"github.com/astaxie/beego/orm"
	"k8s.io/klog/v2"

	"github.com/kubeedge/beehive/pkg/core"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/kubeedge/edge/pkg/common/modules"
	metamanagerconfig "github.com/kubeedge/kubeedge/edge/pkg/metamanager/config"
	"github.com/kubeedge/kubeedge/edge/pkg/metamanager/dao"
	v2 "github.com/kubeedge/kubeedge/edge/pkg/metamanager/dao/v2"
	"github.com/kubeedge/kubeedge/edge/pkg/metamanager/metaserver"
	metaserverconfig "github.com/kubeedge/kubeedge/edge/pkg/metamanager/metaserver/config"
	"github.com/kubeedge/kubeedge/edge/pkg/metamanager/metaserver/kubernetes/storage/sqlite/imitator"
	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/edgecore/v1alpha2"
)

type metaManager struct {
	enable bool
	nodeIP string
}

var _ core.Module = (*metaManager)(nil)

func newMetaManager(enable bool, nodeIP string) *metaManager {
	return &metaManager{
		enable: enable,
		nodeIP: nodeIP,
	}
}

// Register register metamanager
func Register(metaManager *v1alpha2.MetaManager, nodeIP string) {
	metamanagerconfig.InitConfigure(metaManager)
	meta := newMetaManager(metaManager.Enable, nodeIP)
	initDBTable(meta)
	core.Register(meta)
}

// initDBTable create table
func initDBTable(module core.Module) {
	klog.Infof("Begin to register %v db model", module.Name())
	if !module.Enable() {
		klog.Infof("Module %s is disabled, DB meta for it will not be registered", module.Name())
		return
	}
	orm.RegisterModel(new(dao.Meta))
	orm.RegisterModel(new(v2.MetaV2))
}

func (*metaManager) Name() string {
	return modules.MetaManagerModuleName
}

func (*metaManager) Group() string {
	return modules.MetaGroup
}

func (m *metaManager) Enable() bool {
	return m.enable
}

func (m *metaManager) Start() {
	quit := make(chan int) // local quit signal
	if metaserverconfig.Config.Enable {
		imitator.StorageInit()
		go metaserver.NewMetaServer(m.nodeIP).Start(beehiveContext.Done(), quit, false)
	}

	// m.runMetaManager()
	for {
		select {
		case <-beehiveContext.Done():
			klog.Warning("MetaManager main loop stop")
			return
		default:
		}
		msg, err := beehiveContext.Receive(m.Name())
		if err != nil {
			klog.Errorf("get a message %+v: %v", msg, err)
			continue
		}
		if msg.Content == "restartMetaServerPublic" {
			if metaserverconfig.Config.Enable {
				quit <- 1
				time.Sleep(2 * time.Second)
				imitator.StorageInit()
				go metaserver.NewMetaServer(m.nodeIP).Start(beehiveContext.Done(), quit, true)
				resp := msg.NewRespByMessage(&msg, "OK")
				beehiveContext.SendResp(*resp)
			}
		} else if msg.Content == "restartMetaServerPrivate" {
			if metaserverconfig.Config.Enable {
				quit <- 1
				imitator.StorageInit()
				go metaserver.NewMetaServer(m.nodeIP).Start(beehiveContext.Done(), quit, false)
				resp := msg.NewRespByMessage(&msg, "OK")
				beehiveContext.SendResp(*resp)
			}
		}
		// klog.Infof("metamanager get a message %+v", msg)
		klog.V(2).Infof("get a message %+v", msg)
		m.process(msg)
	}
}

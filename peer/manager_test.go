package peer

import (
	"errors"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.fleta.io/fleta/common"
	"git.fleta.io/fleta/framework/log"
	"git.fleta.io/fleta/framework/message"
	"git.fleta.io/fleta/framework/peer/peermessage"
	"git.fleta.io/fleta/framework/router"
)

var (
	testID   int32
	testPort = 3000
	testLock sync.Mutex
)

type testMessage struct {
	peermessage.PeerList
	pm *manager
}

var testMessageType message.Type

func init() {
	testMessageType = message.DefineType("testMessage")
}

func (p *testMessage) Type() message.Type {
	return testMessageType
}

func Test_manager_BroadCast(t *testing.T) {
	testLock.Lock()
	defer testLock.Unlock()
	ID := int(atomic.AddInt32(&testID, 1))
	size := 20
	path := "./test/Test" + strconv.Itoa(ID)
	port := testPort + ID
	os.RemoveAll(path)

	type args struct {
		ChainCoord          *common.Coordinate
		DefaultRouterConfig *router.Config
		DefaultConfig       *Config
		IDs                 []int
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr error
	}{
		{
			name: "string",
			args: args{
				ChainCoord: &common.Coordinate{},
				DefaultRouterConfig: &router.Config{
					Network:   "mock:",
					Port:      port,
					StorePath: path + "/router",
				},
				DefaultConfig: &Config{
					BanEvilScore: 1000,
					StorePath:    path + "/peer",
				},
				IDs: func() []int {
					IDs := make([]int, 0, size)
					for i := 0; i < size; i++ {
						IDs = append(IDs, int(atomic.AddInt32(&testID, 1)))
					}
					return IDs
				}(),
			},
			want:    true,
			wantErr: router.ErrCanNotConnectToEvilNode,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wg := sync.WaitGroup{}
			wg.Add(size)

			creator := func(id int) *testMessage {
				rc := &router.Config{
					Network:   tt.args.DefaultRouterConfig.Network + "testid" + strconv.Itoa(id),
					Port:      tt.args.DefaultRouterConfig.Port,
					StorePath: tt.args.DefaultRouterConfig.StorePath + strconv.Itoa(id) + "/",
				}
				pc := &Config{
					BanEvilScore: tt.args.DefaultConfig.BanEvilScore,
					StorePath:    tt.args.DefaultConfig.StorePath + strconv.Itoa(id) + "/",
				}
				r, _ := router.NewRouter(rc)
				mm := message.NewManager()
				pm, _ := NewManager(tt.args.ChainCoord, r, mm, pc)

				pm.RegisterEventHandler(&BaseEventHandler{})

				tm := &testMessage{
					pm: pm.(*manager),
				}
				tm.List = map[string]peermessage.ConnectInfo{}
				func(tm *testMessage) {
					mm.ApplyMessage(testMessageType, func(r io.Reader) message.Message {
						tm := &testMessage{}
						tm.ReadFrom(r)
						return tm
					}, func(m message.Message) error {
						if t, ok := m.(*testMessage); ok {
							if len(tm.List) == 0 {
								wg.Done()
								tm.From = t.From
								tm.List[strconv.Itoa(len(tm.List))] = peermessage.ConnectInfo{
									Address: t.From,
								}
								tm.pm.BroadCast(tm)
							}

							return nil
						}
						return errors.New("is not test message")
					})
				}(tm)

				return tm
			}

			tms := make([]*testMessage, 0, size)
			for _, id := range tt.args.IDs {
				tm := creator(id)
				tm.pm.StartManage()
				tm.pm.AddNode("testid" + strconv.Itoa(tt.args.IDs[0]))
				tms = append(tms, tm)
			}

			for len(tms[len(tms)-1].pm.GroupList()) < 6 {
				log.Info(len(tms[len(tms)-1].pm.GroupList()))
				time.Sleep(time.Second)
			}

			log.Info("BroadCast init done")

			tms[len(tms)-1].From = "send broadCast"
			tms[len(tms)-1].pm.BroadCast(tms[len(tms)-1])

			wg.Wait()

			count := 0
			for _, tm := range tms {
				for key, ci := range tm.List {
					log.Info("for key ", key, ", ", ci.Address)
					str := ci.Address
					if str == "send broadCast" {
						count++
					}
				}
			}

			if count < size {
				t.Errorf("received count %v", count)
				return
			}

		})
	}
}

func Test_manager_ExceptCast(t *testing.T) {
	testLock.Lock()
	defer testLock.Unlock()
	ID := int(atomic.AddInt32(&testID, 1))
	size := 20
	path := "./test/Test" + strconv.Itoa(ID)
	port := testPort + ID
	os.RemoveAll(path)

	type args struct {
		ChainCoord          *common.Coordinate
		DefaultRouterConfig *router.Config
		DefaultConfig       *Config
		IDs                 []int
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr error
	}{
		{
			name: "string",
			args: args{
				ChainCoord: &common.Coordinate{},
				DefaultRouterConfig: &router.Config{
					Network:   "mock:",
					Port:      port,
					StorePath: path + "/router",
				},
				DefaultConfig: &Config{
					BanEvilScore: 1000,
					StorePath:    path + "/peer",
				},
				IDs: func() []int {
					IDs := make([]int, 0, size)
					for i := 0; i < size; i++ {
						IDs = append(IDs, int(atomic.AddInt32(&testID, 1)))
					}
					return IDs
				}(),
			},
			want:    true,
			wantErr: router.ErrCanNotConnectToEvilNode,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			wg := sync.WaitGroup{}
			wg.Add(size - 1)

			creator := func(id int) *testMessage {
				rc := &router.Config{
					Network:   tt.args.DefaultRouterConfig.Network + "testid" + strconv.Itoa(id),
					Port:      tt.args.DefaultRouterConfig.Port,
					StorePath: tt.args.DefaultRouterConfig.StorePath + strconv.Itoa(id) + "/",
				}
				pc := &Config{
					BanEvilScore: tt.args.DefaultConfig.BanEvilScore,
					StorePath:    tt.args.DefaultConfig.StorePath + strconv.Itoa(id) + "/",
				}
				r, _ := router.NewRouter(rc)
				mm := message.NewManager()
				pm, _ := NewManager(tt.args.ChainCoord, r, mm, pc)

				pm.RegisterEventHandler(&BaseEventHandler{})

				tm := &testMessage{
					pm: pm.(*manager),
				}
				tm.List = map[string]peermessage.ConnectInfo{}
				func(tm *testMessage) {
					mm.ApplyMessage(testMessageType, func(r io.Reader) message.Message {
						tm := &testMessage{}
						tm.ReadFrom(r)
						return tm
					}, func(m message.Message) error {
						if t, ok := m.(*testMessage); ok {
							if len(tm.List) == 0 {
								wg.Done()
								tm.List[strconv.Itoa(len(tm.List))] = peermessage.ConnectInfo{
									Address: t.From,
								}
								tm.From = t.From
								tm.pm.ExceptCast(tm.From, tm)
							}
							return nil
						}
						return errors.New("is not test message")
					})
				}(tm)

				return tm
			}

			tms := make([]*testMessage, 0, size)
			for _, id := range tt.args.IDs {
				tm := creator(id)
				tm.pm.StartManage()
				tm.pm.AddNode("testid" + strconv.Itoa(tt.args.IDs[0]))
				tms = append(tms, tm)
			}

			for len(tms[len(tms)-1].pm.GroupList()) < 6 {
				log.Info(len(tms[len(tms)-1].pm.GroupList()))
				time.Sleep(time.Second)
			}

			log.Info("ExceptCast init done")
			exceptNode := tms[1].pm.router.Localhost()
			log.Info("start except cast except node : ", exceptNode)

			tms[len(tms)-1].From = exceptNode
			tms[len(tms)-1].pm.ExceptCast(exceptNode, tms[len(tms)-1])

			count := 0
			wg.Wait()

			for _, tm := range tms {
				for key, ci := range tm.List {
					log.Info("for key ", key, ", ", ci.Address)
					str := ci.Address
					if str == exceptNode {
						count++
					}
				}
			}

			if count != size-1 {
				t.Errorf("received count not match expect %v real %v", size-1, count)
				return
			}

			if tms[1].From != "" && exceptNode != tms[len(tms)-1].From {
				t.Errorf("except target %v but received data %v", exceptNode, tms[1].From)
				return
			}

		})
	}
}

func TestNewManager(t *testing.T) {
	testLock.Lock()
	defer testLock.Unlock()
	ID := int(atomic.AddInt32(&testID, 1))
	addr := "test" + strconv.Itoa(ID)
	path := "./test/Test" + strconv.Itoa(ID)
	port := testPort + ID
	os.RemoveAll(path)

	type args struct {
		ChainCoord   *common.Coordinate
		routerConfig *router.Config
		Config       *Config
		mm           *message.Manager
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "string",
			args: args{
				ChainCoord: &common.Coordinate{},
				routerConfig: &router.Config{
					Network:   "mock:" + addr,
					Port:      port,
					StorePath: path + "/router/",
				},
				Config: &Config{
					BanEvilScore: 100,
					StorePath:    path + "/peer/",
				},
				mm: message.NewManager(),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := router.NewRouter(tt.args.routerConfig)
			_, err := NewManager(tt.args.ChainCoord, r, tt.args.mm, tt.args.Config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestAddNode(t *testing.T) {
	testLock.Lock()
	defer testLock.Unlock()
	ID1 := int(atomic.AddInt32(&testID, 1))
	ID2 := int(atomic.AddInt32(&testID, 1))
	addr1 := "test" + strconv.Itoa(ID1)
	addr2 := "test" + strconv.Itoa(ID2)
	path := "./test/Test" + strconv.Itoa(ID1)
	port := testPort + ID1
	os.RemoveAll(path)

	type args struct {
		ChainCoord    *common.Coordinate
		routerConfig1 *router.Config
		routerConfig2 *router.Config
		Config1       *Config
		Config2       *Config
		mm            *message.Manager
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "string",
			args: args{
				ChainCoord: &common.Coordinate{},
				routerConfig1: &router.Config{
					Network:   "mock:" + addr1,
					Port:      port,
					StorePath: path + "/router1/",
				},
				Config1: &Config{
					BanEvilScore: 100,
					StorePath:    path + "/peer1/",
				},
				routerConfig2: &router.Config{
					Network:   "mock:" + addr2,
					Port:      port,
					StorePath: path + "/router2/",
				},
				Config2: &Config{
					BanEvilScore: 100,
					StorePath:    path + "/peer2/",
				},
				mm: message.NewManager(),
			},
			want:    true,
			wantErr: false,
		},
	}
	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			addr1 = addr1 + ":" + strconv.Itoa(port)
			addr2 = addr2 + ":" + strconv.Itoa(port)
			r1, _ := router.NewRouter(tt.args.routerConfig1)
			pm1, _ := NewManager(tt.args.ChainCoord, r1, tt.args.mm, tt.args.Config1)

			r2, _ := router.NewRouter(tt.args.routerConfig2)
			pm2, _ := NewManager(tt.args.ChainCoord, r2, tt.args.mm, tt.args.Config2)

			err := pm2.AddNode(addr1)

			pm1.StartManage()
			pm2.StartManage()

			pm1.EnforceConnect()
			pm2.EnforceConnect()

			{
				for {
					for _, addr := range pm1.ConnectedList() {
						if addr == addr2 {
							goto EndFor
						}
					}
					time.Sleep(time.Second)
				}
			EndFor:
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestBanEvil(t *testing.T) {
	testLock.Lock()
	defer testLock.Unlock()
	ID := int(atomic.AddInt32(&testID, 1))
	addr := "test" + strconv.Itoa(ID)
	path := "./test/Test" + strconv.Itoa(ID)
	port := testPort + ID
	os.RemoveAll(path)

	type args struct {
		ChainCoord    *common.Coordinate
		routerConfig1 *router.Config
		Config1       *Config
		mm            *message.Manager
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr error
	}{
		{
			name: "string",
			args: args{
				ChainCoord: &common.Coordinate{},
				routerConfig1: &router.Config{
					Network:   "mock:" + addr,
					Port:      port,
					StorePath: path + "/router1/",
				},
				Config1: &Config{
					BanEvilScore: 100,
					StorePath:    path + "/peer1/",
				},
				mm: message.NewManager(),
			},
			want:    true,
			wantErr: router.ErrCanNotConnectToEvilNode,
		},
	}
	for _, tt := range tests {
		tempAddr := "temp:" + strconv.Itoa(port)
		t.Run(tt.name, func(t *testing.T) {
			r1, _ := router.NewRouter(tt.args.routerConfig1)
			pm, _ := NewManager(tt.args.ChainCoord, r1, tt.args.mm, tt.args.Config1)
			pm1 := pm.(*manager)
			err := pm1.AddNode(tempAddr)
			pm1.StartManage()
			pm1.EnforceConnect()
			time.Sleep(time.Millisecond * 100)
			err = pm1.doManageCandidate(tempAddr, csPunishableRequestWait)
			time.Sleep(time.Millisecond * 100)
			err = pm1.doManageCandidate(tempAddr, csPunishableRequestWait)
			time.Sleep(time.Millisecond * 100)
			err = pm1.doManageCandidate(tempAddr, csPunishableRequestWait)
			time.Sleep(time.Millisecond * 100)

			if err != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			err = pm1.AddNode(tempAddr)
			if err != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestPeerListSpread(t *testing.T) {
	testLock.Lock()
	defer testLock.Unlock()
	ID := int(atomic.AddInt32(&testID, 1))
	size := 20
	path := "./test/Test" + strconv.Itoa(ID)
	port := testPort + ID
	os.RemoveAll(path)

	type args struct {
		ChainCoord          *common.Coordinate
		DefaultRouterConfig *router.Config
		DefaultConfig       *Config
		IDs                 []int
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr error
	}{
		{
			name: "string",
			args: args{
				ChainCoord: &common.Coordinate{},
				DefaultRouterConfig: &router.Config{
					Network:   "mock:",
					Port:      port,
					StorePath: path + "/router",
				},
				DefaultConfig: &Config{
					BanEvilScore: 100,
					StorePath:    path + "/peer",
				},
				IDs: func() []int {
					IDs := make([]int, 0, size)
					for i := 0; i < size; i++ {
						IDs = append(IDs, int(atomic.AddInt32(&testID, 1)))
					}
					return IDs
				}(),
			},
			want:    true,
			wantErr: router.ErrCanNotConnectToEvilNode,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			creator := func(id int) Manager {
				rc := &router.Config{
					Network:   tt.args.DefaultRouterConfig.Network + "testid" + strconv.Itoa(id),
					Port:      tt.args.DefaultRouterConfig.Port,
					StorePath: tt.args.DefaultRouterConfig.StorePath + strconv.Itoa(id) + "/",
				}
				pc := &Config{
					BanEvilScore: tt.args.DefaultConfig.BanEvilScore,
					StorePath:    tt.args.DefaultConfig.StorePath + strconv.Itoa(id) + "/",
				}
				r, _ := router.NewRouter(rc)
				mm := message.NewManager()
				pm, _ := NewManager(tt.args.ChainCoord, r, mm, pc)

				pm.RegisterEventHandler(&BaseEventHandler{})

				return pm
			}

			pms := make([]*manager, 0, size)
			for _, id := range tt.args.IDs {
				pm := creator(id)
				pm.StartManage()
				pm.AddNode("testid" + strconv.Itoa(tt.args.IDs[0]))
				Pm := pm.(*manager)
				pms = append(pms, Pm)
			}

			for len(pms[len(pms)-1].NodeList()) < size-1 {
				time.Sleep(time.Second)
			}

			log.Info("NodeList fill done")

			for len(pms[len(pms)-1].GroupList()) < 6 {
				time.Sleep(time.Second)
			}

			log.Info("GroupList fill done")

		})
	}
}

func Test_manager_EnforceConnect(t *testing.T) {
	testLock.Lock()
	defer testLock.Unlock()
	ID1 := int(atomic.AddInt32(&testID, 1))
	ID2 := int(atomic.AddInt32(&testID, 1))
	addr1 := "test" + strconv.Itoa(ID1)
	addr2 := "test" + strconv.Itoa(ID2)
	path := "./test/Test" + strconv.Itoa(ID1)
	port := testPort + ID1
	os.RemoveAll(path)

	type args struct {
		ChainCoord    *common.Coordinate
		routerConfig1 *router.Config
		routerConfig2 *router.Config
		Config1       *Config
		Config2       *Config
		mm            *message.Manager
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "string",
			args: args{
				ChainCoord: &common.Coordinate{},
				routerConfig1: &router.Config{
					Network:   "mock:" + addr1,
					Port:      port,
					StorePath: path + "/router1/",
				},
				Config1: &Config{
					BanEvilScore: 100,
					StorePath:    path + "/peer1/",
				},
				routerConfig2: &router.Config{
					Network:   "mock:" + addr2,
					Port:      port,
					StorePath: path + "/router2/",
				},
				Config2: &Config{
					BanEvilScore: 100,
					StorePath:    path + "/peer2/",
				},
				mm: message.NewManager(),
			},
			want:    true,
			wantErr: false,
		},
	}
	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			addr1 = addr1 + ":" + strconv.Itoa(port)
			addr2 = addr2 + ":" + strconv.Itoa(port)
			r1, _ := router.NewRouter(tt.args.routerConfig1)
			pm1, _ := NewManager(tt.args.ChainCoord, r1, tt.args.mm, tt.args.Config1)

			r2, _ := router.NewRouter(tt.args.routerConfig2)
			pm2, _ := NewManager(tt.args.ChainCoord, r2, tt.args.mm, tt.args.Config2)

			err := pm2.AddNode(addr1)

			time.Sleep(time.Second * 6)

			pm1.StartManage()
			pm2.StartManage()

			pm2.EnforceConnect()

			{
				for {
					for _, addr := range pm1.ConnectedList() {
						if addr == addr2 {
							goto EndFor
						}
					}
					time.Sleep(time.Second)
				}
			EndFor:
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

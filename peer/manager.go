package peer

import (
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.fleta.io/fleta/framework/chain/mesh"
	"git.fleta.io/fleta/framework/router/evilnode"

	"git.fleta.io/fleta/common"
	"git.fleta.io/fleta/framework/log"
	"git.fleta.io/fleta/framework/message"
	"git.fleta.io/fleta/framework/peer/peermessage"
	"git.fleta.io/fleta/framework/peer/storage"
	"git.fleta.io/fleta/framework/router"
)

//Config is structure storing settings information
type Config struct {
	StorePath string
}

// peer errors
var (
	ErrNotFoundPeer = errors.New("not found peer")
)

//Manager manages peer-connected networks.
type Manager interface {
	RegisterEventHandler(eh mesh.EventHandler)
	StartManage()
	EnforceConnect()
	AddNode(addr string) error
	BroadCast(m message.Message)
	NodeList() []string
	ConnectedList() []string
	TargetCast(addr string, m message.Message) error
	ExceptCast(addr string, m message.Message)
}

type manager struct {
	Config         *Config
	ChainCoord     *common.Coordinate
	router         router.Router
	MessageManager *message.Manager
	onReady        func(p *peer)

	nodes           *nodeStore
	nodeRotateIndex int
	candidates      candidateMap

	peerGroupLock sync.Mutex
	connections   connectMap

	peerStorage storage.PeerStorage

	BaseEventHandler

	eventHandlerLock sync.RWMutex
	eventHandler     []mesh.EventHandler
	BanPeerInfos     *ByTime
}

type candidateState int

const (
	csRequestWait           candidateState = 1
	csPunishableRequestWait candidateState = 2
	csPeerListWait          candidateState = 3
)

//NewManager is the peerManager creator.
//Apply messages necessary for peer management.
func NewManager(ChainCoord *common.Coordinate, r router.Router, Config *Config) (*manager, error) {
	ns, err := newNodeStore(Config.StorePath)
	if err != nil {
		return nil, err
	}
	pm := &manager{
		Config:         Config,
		ChainCoord:     ChainCoord,
		router:         r,
		MessageManager: message.NewManager(),
		nodes:          ns,
		candidates:     candidateMap{},
		connections:    connectMap{},
		eventHandler:   []mesh.EventHandler{},
		BanPeerInfos:   NewByTime(),
	}
	pm.peerStorage = storage.NewPeerStorage(pm.kickOutPeerStorage)

	//add requestPeerList message
	pm.MessageManager.SetCreator(peermessage.PeerListMessageType, peermessage.PeerListCreator)
	// mm.ApplyMessage(peermessage.PeerListMessageType, peermessage.PeerListCreator, pm.peerListHandler)

	pm.RegisterEventHandler(pm)

	return pm, nil
}

func (pm *manager) errLog(msg ...interface{}) {
	var file string
	var line int
	{
		pc := make([]uintptr, 10) // at least 1 entry needed
		runtime.Callers(2, pc)
		f := runtime.FuncForPC(pc[0])
		file, line = f.FileLine(pc[0])

		path := strings.Split(file, "/")
		file = strings.Join(path[len(path)-3:], "/")
	}
	log.Error(append([]interface{}{file, " ", line, " ", pm.router.Conf().Network, " "}, msg...)...)
}

//RegisterEventHandler is Registered event handler
func (pm *manager) RegisterEventHandler(eh mesh.EventHandler) {
	pm.eventHandlerLock.Lock()
	pm.eventHandler = append(pm.eventHandler, eh)
	pm.eventHandlerLock.Unlock()
}

//StartManage is start peer management
func (pm *manager) StartManage() {
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		wg.Done()
		for {
			conn, pingTime, err := pm.router.Accept(pm.ChainCoord)
			if err != nil {
				pm.errLog(err, conn.RemoteAddr().String())
				continue
			}

			// ban check
			addr := conn.RemoteAddr().String()
			if pm.BanPeerInfos.IsBan(addr) {
				if hp, has := pm.connections.Load(addr); has {
					hp.Close()
				}
				conn.Close()
				pm.errLog("BanPeerInfos.IsBan(addr) ", addr)
				continue
			}

			go func(conn net.Conn) {
				peer := newPeer(conn, pingTime, pm.deletePeer, pm.onRecvEventHandler)
				pm.eventHandlerLock.RLock()
				var err error
				for _, eh := range pm.eventHandler {
					err = eh.BeforeConnect(peer)
					if err != nil {
						pm.errLog("BeforeConnect(peer) err ", err)
						break
					}
				}
				pm.eventHandlerLock.RUnlock()
				if err != nil {
					pm.errLog("StartManage BeforeConnect event err ", err)
					peer.Close()
					return
				}
				err = pm.addPeer(peer)
				if err != nil {
					pm.errLog("StartManage addPeer err ", err)
					return
				}
				pm.eventHandlerLock.RLock()
				for _, eh := range pm.eventHandler {
					eh.AfterConnect(peer)
				}
				pm.eventHandlerLock.RUnlock()

			}(conn)
		}
	}()
	wg.Wait()

	pm.router.AddListen(pm.ChainCoord)

	go pm.manageCandidate()
	go pm.rotatePeer()
}

func (pm *manager) onRecvEventHandler(p *peer, t message.Type) error {
	pm.eventHandlerLock.RLock()
	defer pm.eventHandlerLock.RUnlock()
	for _, eh := range pm.eventHandler {
		err := eh.OnRecv(p, p, t)
		if err != nil {
			if err == message.ErrUnknownMessage {
				pm.errLog("onRecvEventHandler message.ErrUnknownMessage local ", p.LocalAddr().String(), "remote", p.RemoteAddr().String())
				continue
			}
			pm.errLog("onRecvEventHandler ", err, " local ", p.LocalAddr().String(), "remote", p.RemoteAddr().String())
			return err
		}
		break
	}

	return nil
}

// EnforceConnect handles all of the Request standby nodes in the cardidate.
func (pm *manager) EnforceConnect() {
	dialList := []string{}
	pm.candidates.rangeMap(func(addr string, cs candidateState) bool {
		if cs == csRequestWait || cs == csPunishableRequestWait {
			dialList = append(dialList, addr)
		}
		return true
	})
	for _, addr := range dialList {
		err := pm.router.Request(addr, pm.ChainCoord)
		if err != nil {
			pm.errLog("EnforceConnect error ", err)
		}
		time.Sleep(time.Millisecond * 50)
	}
}

// AddNode is used to register additional peers from outside.
func (pm *manager) AddNode(addr string) error {
	if pm.router.Localhost() != "" && strings.HasPrefix(addr, pm.router.Localhost()) {
		return nil
	}

	if !pm.router.EvilNodeManager().IsBanNode(addr) {
		pm.candidates.store(addr, csRequestWait)
		go pm.doManageCandidate(addr, csRequestWait)
		log.Debug("AddNode ", pm.router.Localhost(), addr)
	} else {
		pm.errLog("AddNode router.ErrCanNotConnectToEvilNode", addr)
		return router.ErrCanNotConnectToEvilNode
	}
	return nil
}

//BroadCast is used to propagate messages to all nodes.
func (pm *manager) BroadCast(m message.Message) {
	pm.connections.Range(func(addr string, p Peer) bool {
		p.Send(m)
		return true
	})
}

//BroadCast is used to propagate messages to all nodes.
func (pm *manager) ExceptCast(exceptAddr string, m message.Message) {
	pm.connections.Range(func(addr string, p Peer) bool {
		if exceptAddr != addr {
			p.Send(m)
		}
		return true
	})
}

//NodeList is returns the addresses of the collected peers
func (pm *manager) NodeList() []string {
	list := make([]string, 0)
	pm.nodes.Range(func(addr string, ci peermessage.ConnectInfo) bool {
		list = append(list, addr+":"+strconv.Itoa(ci.PingScoreBoard.Len()))
		return true
	})
	return list
}

//CandidateList is returns the address of the node waiting for the operation.
func (pm *manager) ConnectedList() []string {
	list := make([]string, pm.connections.Len())
	pm.connections.Range(func(addr string, p Peer) bool {
		list = append(list, addr)
		return true
	})
	return list
}

//CandidateList is returns the address of the node waiting for the operation.
func (pm *manager) TargetCast(addr string, m message.Message) error {
	if p, has := pm.connections.Load(addr); has {
		p.Send(m)
		return nil
	}
	return ErrNotFoundPeer
}

//GroupList returns a list of peer groups.
func (pm *manager) GroupList() []string {
	return pm.peerStorage.List()
}

// func (pm *manager) peerListHandler(m message.Message) error {
func (pm *manager) OnRecv(p mesh.Peer, r io.Reader, t message.Type) error {
	m, err := pm.MessageManager.ParseMessage(r, t)
	if err != nil {
		pm.errLog(err, p.ID())
		return err
	}

	switch m.(type) {
	case *peermessage.PeerList:
		peerList := m.(*peermessage.PeerList)
		if peerList.Request == true {
			peerList.Request = false
			nodeMap := make(map[string]peermessage.ConnectInfo)
			pm.nodes.Range(func(addr string, ci peermessage.ConnectInfo) bool {
				nodeMap[addr] = ci
				return true
			})
			peerList.List = nodeMap

			if p, has := pm.connections.Load(peerList.From); has {
				peerList.From = p.LocalAddr().String()
				p.Send(peerList)
			}

		} else {
			pm.peerGroupLock.Lock()
			pm.candidates.delete(peerList.From)

			for _, ci := range peerList.List {
				if ci.Address == pm.router.Localhost() {
					continue
				}

				if _, has := pm.candidates.load(ci.Address); has {
					continue
				}
				if _, has := pm.nodes.Load(ci.Address); has {
					continue
				}
				if _, has := pm.connections.Load(ci.Address); has {
					continue
				}

				pm.AddNode(ci.Address)
			}

			if p, connectionHas := pm.connections.Load(peerList.From); connectionHas {
				for _, ci := range peerList.List {
					if connectionHas {
						pm.updateScoreBoard(p, ci)
					}
				}
				pm.addReadyConn(p)
			}

			pm.peerGroupLock.Unlock()
		}

	}

	return nil
}

func (pm *manager) updateScoreBoard(p Peer, ci peermessage.ConnectInfo) {
	addr := p.NetAddr()

	node := pm.nodes.LoadOrStore(addr, peermessage.NewConnectInfo(addr, p.PingTime()))
	node.PingScoreBoard.Store(ci.Address, ci.PingTime, p.LocalAddr().String()+" "+p.NetAddr()+" ")
}

func (pm *manager) doManageCandidate(addr string, cs candidateState) error {
	if strings.HasPrefix(addr, pm.router.Localhost()) {
		go pm.candidates.delete(addr)
	}
	var err error
	switch cs {
	case csRequestWait:
		err = pm.router.Request(addr, pm.ChainCoord)
		log.Debug(pm.router.Conf().Network, addr)
		go pm.candidates.store(addr, csPunishableRequestWait)
		if err != nil {
			pm.errLog("RequestWait err ", err)
		}
	case csPunishableRequestWait:
		err = pm.router.Request(addr, pm.ChainCoord)
		if err != nil {
			pm.router.EvilNodeManager().TellOn(addr, evilnode.DialFail)
			pm.errLog("TellOn ", err)
		}
	case csPeerListWait:
		if p, has := pm.connections.Load(addr); has {
			peermessage.SendRequestPeerList(p, p.LocalAddr().String())
		} else {
			go pm.candidates.store(addr, csRequestWait)
		}
	}
	return err
}

func (pm *manager) manageCandidate() {
	for {
		time.Sleep(time.Second * 30)
		pm.candidates.rangeMap(func(addr string, cs candidateState) bool {
			pm.doManageCandidate(addr, cs)
			time.Sleep(time.Millisecond * 50)
			return true
		})
	}
}

func (pm *manager) rotatePeer() {
	for {
		if pm.peerStorage.NotEnoughPeer() {
			time.Sleep(time.Second * 2)
		} else {
			time.Sleep(time.Minute * 20)
		}

		pm.appendPeerStorage()
	}
}

func (pm *manager) appendPeerStorage() {
	if pm.connections.Len() == 0 {
		return
	}

	if pm.connections.Len() == 1 {
		pm.connections.Range(func(k string, p Peer) bool {
			log.Info("send request list ", p.LocalAddr().String(), " to ", p.RemoteAddr().String())
			peermessage.SendRequestPeerList(p, p.LocalAddr().String())
			return false
		})
	}

	for i := pm.nodeRotateIndex; i < pm.nodes.Len(); i++ {
		p := pm.nodes.Get(i)
		pm.nodeRotateIndex = i + 1
		if pm.peerStorage.Have(p.Address) {
			continue
		}
		if connectedPeer, has := pm.connections.Load(p.Address); has {
			pm.addReadyConn(connectedPeer)
		} else {
			err := pm.router.Request(p.Address, pm.ChainCoord)
			if err != nil {
				pm.errLog("PeerListHandler err ", err)
			}
		}

		break
	}
	if pm.nodeRotateIndex >= pm.nodes.Len()-1 {
		pm.nodeRotateIndex = 0
	}
}

func (pm *manager) kickOutPeerStorage(ip storage.Peer) {
	if p, ok := ip.(Peer); ok {
		if pm.connections.Len() > storage.MaxPeerStorageLen()*2 {
			closePeer := p
			pm.connections.Range(func(addr string, p Peer) bool {
				if closePeer.ConnectedTime() > p.ConnectedTime() {
					if !pm.peerStorage.Have(addr) {
						closePeer = p
					}
				}
				return true
			})
			closePeer.Close()
		}
	}
}

func (pm *manager) deletePeer(addr string) {
	pm.eventHandlerLock.RLock()
	if p, has := pm.connections.Load(addr); has {
		for _, eh := range pm.eventHandler {
			eh.OnClosed(p)
		}
	}
	pm.eventHandlerLock.RUnlock()
	pm.connections.Delete(addr)
}

func (pm *manager) addPeer(p Peer) error {
	pm.errLog("addPeer ", p.RemoteAddr().String())
	pm.peerGroupLock.Lock()
	defer pm.peerGroupLock.Unlock()

	if _, has := pm.connections.Load(p.NetAddr()); has {
		p.Close()
		pm.errLog("addPeer, ", ErrIsAlreadyConnected, p.RemoteAddr().String())
		return ErrIsAlreadyConnected
	} else {
		addr := p.NetAddr() // .RemoteAddr().String()
		pm.connections.Store(addr, p)
		pm.nodes.Store(addr, peermessage.NewConnectInfo(addr, p.PingTime()))
		pm.errLog("nodes.Store, ", addr)
		pm.candidates.store(addr, csPeerListWait)

		go func(p Peer) {
			peermessage.SendRequestPeerList(p, p.LocalAddr().String())
		}(p)
	}
	return nil
}

func (pm *manager) addReadyConn(p Peer) {
	pm.peerStorage.Add(p, func(addr string) (time.Duration, bool) {
		if node, has := pm.nodes.Load(addr); has {
			return node.PingScoreBoard.Load(addr)
		}
		return 0, false
	})

}

/***
 * implament of mage interface
**/
// AddNode is used to register additional peers from outside.
func (pm *manager) Add(netAddr string, doForce bool) {
	err := pm.router.Request(netAddr, pm.ChainCoord)
	pm.candidates.store(netAddr, csPunishableRequestWait)
	if err != nil {
		pm.errLog("RequestWait err ", err, netAddr)
	}
}

func (pm *manager) Remove(netAddr string) {
	p, has := pm.connections.Load(netAddr)
	if has {
		p.Close()
	}
}
func (pm *manager) RemoveByID(ID string) {
	pm.Remove(ID)
}

type BanPeerInfo struct {
	NetAddr  string
	Timeout  int64
	OverTime int64
}

func (p BanPeerInfo) String() string {
	return fmt.Sprintf("%s Ban over %d", p.NetAddr, p.OverTime)
}

// ByTime implements sort.Interface for []Person BanPeerInfo on
// the Timeout field.
type ByTime struct {
	Arr []*BanPeerInfo
	Map map[string]*BanPeerInfo
}

func NewByTime() *ByTime {
	return &ByTime{
		Arr: []*BanPeerInfo{},
		Map: map[string]*BanPeerInfo{},
	}
}

func (a *ByTime) Len() int           { return len(a.Arr) }
func (a *ByTime) Swap(i, j int)      { a.Arr[i], a.Arr[j] = a.Arr[j], a.Arr[i] }
func (a *ByTime) Less(i, j int) bool { return a.Arr[i].Timeout < a.Arr[j].Timeout }

func (a *ByTime) Add(netAddr string, Seconds int64) {
	b, has := a.Map[netAddr]
	if !has {
		b = &BanPeerInfo{
			NetAddr:  netAddr,
			Timeout:  time.Now().UnixNano() + (int64(time.Second) * Seconds),
			OverTime: Seconds,
		}
		a.Arr = append(a.Arr, b)
		a.Map[netAddr] = b
	} else {
		b.Timeout = Seconds
	}
	sort.Sort(a)
}

func (a *ByTime) Delete(netAddr string) {
	i := a.Search(netAddr)
	if i < 0 {
		return
	}

	b := a.Arr[i]
	a.Arr = append(a.Arr[:i], a.Arr[i+1:]...)
	delete(a.Map, b.NetAddr)
}

func (a *ByTime) Search(netAddr string) int {
	b, has := a.Map[netAddr]
	if !has {
		return -1
	}

	i, j := 0, len(a.Arr)
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j
		if !(a.Arr[h].Timeout >= b.Timeout) {
			i = h + 1 // preserves f(i-1) == false
		} else {
			j = h // preserves f(j) == true
		}
	}
	// i == j, f(i-1) == false, and f(j) (= f(i)) == true  =>  answer is i.
	return i
}

func (a *ByTime) IsBan(netAddr string) bool {
	now := time.Now().UnixNano()
	var slicePivot = 0
	for i, b := range a.Arr {
		if now < b.Timeout {
			slicePivot = i
			break
		}
		delete(a.Map, a.Arr[i].NetAddr)
	}

	a.Arr = a.Arr[slicePivot:]
	_, has := a.Map[netAddr]
	return has
}

func (pm *manager) Ban(netAddr string, Seconds uint32) {
	pm.BanPeerInfos.Add(netAddr, int64(Seconds))
	p, has := pm.connections.Load(netAddr)
	if has {
		p.Close()
	}
}

func (pm *manager) BanByID(ID string, Seconds uint32) {
	pm.Ban(ID, Seconds)
}

func (pm *manager) Unban(netAddr string) {
	pm.BanPeerInfos.Delete(netAddr)
}

func (pm *manager) Peers() []mesh.Peer {
	list := make([]mesh.Peer, 0)
	pm.connections.Range(func(addr string, p Peer) bool {
		list = append(list, p)
		return true
	})
	return list
}

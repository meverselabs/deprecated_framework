package router

import (
	"net"
	"sync"
	"testing"
	"time"

	"git.fleta.io/fleta/common"
	"git.fleta.io/fleta/framework/log"
)

func Test_removePort(t *testing.T) {
	type args struct {
		addr string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr error
	}{
		{
			name: "string",
			args: args{
				addr: "test:test",
			},
			want:    "test:test",
			wantErr: ErrNotFoundPort,
		},
		{
			name: "num",
			args: args{
				addr: "test:123",
			},
			want:    "test",
			wantErr: nil,
		},
		{
			name: "notincludeport",
			args: args{
				addr: "test",
			},
			want:    "test",
			wantErr: ErrNotFoundPort,
		},
		{
			name: "multycolon",
			args: args{
				addr: "[test:test:test:test]:123",
			},
			want:    "[test:test:test:test]",
			wantErr: nil,
		},
		{
			name: "multycolonNotNum",
			args: args{
				addr: "[test:test:test:test]:test",
			},
			want:    "[test:test:test:test]:test",
			wantErr: ErrNotFoundPort,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := removePort(tt.args.addr)
			if got != tt.want {
				t.Errorf("removePort() = %v, want %v", got, tt.want)
			}
			if err != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_router_Connecte(t *testing.T) {
	type args struct {
		ChainCoord *common.Coordinate
		Config1    *Config
		Config2    *Config
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
				Config1: &Config{
					Network:   "mock:test1",
					Port:      3000,
					StorePath: "./test/debug1/",
				},
				Config2: &Config{
					Network:   "mock:test2",
					Port:      3000,
					StorePath: "./test/debug2/",
				},
			},
			want:    true,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r1, _ := NewRouter(tt.args.Config1)
			r2, _ := NewRouter(tt.args.Config2)

			r1.AddListen(tt.args.ChainCoord)
			r2.AddListen(tt.args.ChainCoord)

			r2.Request("test1:3000", tt.args.ChainCoord)

			wg := sync.WaitGroup{}
			wg.Add(2)

			var ping1 time.Duration
			var ping2 time.Duration
			go func() {
				_, ping, _ := r1.Accept(tt.args.ChainCoord)
				ping1 = ping
				log.Info("ping1 ", ping1)
				wg.Done()
			}()
			go func() {
				_, ping, _ := r2.Accept(tt.args.ChainCoord)
				ping2 = ping
				log.Info("ping2 ", ping2)
				wg.Done()
			}()
			wg.Wait()

			if ((ping1 > 0) == (ping2 > 0)) != tt.want {
				t.Errorf("ping1 = %v, ping2 = %v, want %v", ping1, ping2, tt.want)
			}
		})
	}
}

func Test_router_Connecte_send(t *testing.T) {
	type args struct {
		ChainCoord *common.Coordinate
		Config1    *Config
		Config2    *Config
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
				Config1: &Config{
					Network:   "mock:send1",
					Port:      3002,
					StorePath: "./test/send1/",
				},
				Config2: &Config{
					Network:   "mock:send2",
					Port:      3002,
					StorePath: "./test/send2/",
				},
			},
			want:    true,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r1, _ := NewRouter(tt.args.Config1)
			r2, _ := NewRouter(tt.args.Config2)

			r1.AddListen(tt.args.ChainCoord)
			r2.AddListen(tt.args.ChainCoord)

			r2.Request("send1:3002", tt.args.ChainCoord)

			wg := sync.WaitGroup{}
			wg.Add(2)

			var readConn net.Conn
			var writeConn net.Conn
			go func() {
				conn, _, _ := r1.Accept(tt.args.ChainCoord)
				readConn = conn
				wg.Done()
			}()
			go func() {
				conn, _, _ := r2.Accept(tt.args.ChainCoord)
				writeConn = conn
				wg.Done()
			}()
			wg.Wait()

			wg.Add(1)
			strChan := make(chan string)
			go func() {
				wg.Wait()
				bs := make([]byte, 1024)
				n, _ := readConn.Read(bs)
				strChan <- string(bs[:n])
			}()

			go func() {
				writeConn.Write([]byte("sendTest"))
				wg.Done()
			}()

			result := <-strChan

			if (result == "sendTest") != tt.want {
				t.Errorf("result = %v, want %v", result, tt.want)
			}
		})
	}
}

func Test_router_Connecte_request_to_local(t *testing.T) {
	type args struct {
		ChainCoord *common.Coordinate
		Config1    *Config
	}
	tests := []struct {
		name    string
		args    args
		wantErr error
	}{
		{
			name: "requesttolocal",
			args: args{
				ChainCoord: &common.Coordinate{},
				Config1: &Config{
					Network:   "mock:requesttolocal",
					Port:      3001,
					StorePath: "./test/debug3/",
				},
			},
			wantErr: ErrCannotRequestToLocal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r1, _ := NewRouter(tt.args.Config1)

			r1.AddListen(tt.args.ChainCoord)

			err := r1.Request("requesttolocal:3001", tt.args.ChainCoord)

			if err != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_router_Time_store(t *testing.T) {
	type args struct {
		period time.Duration
		size   int64
		sleep  time.Duration
		key    string
		value  string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "inTime",
			args: args{
				period: time.Second,
				size:   3,
				sleep:  0,
				key:    "key",
				value:  "value",
			},
			want: true,
		},
		{
			name: "timeout",
			args: args{
				period: time.Second,
				size:   3,
				sleep:  time.Second * 4,
				key:    "key",
				value:  "value",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := true

			m := NewTimerMap(tt.args.period, tt.args.size)

			m.Store(tt.args.key, tt.args.value)
			time.Sleep(tt.args.sleep)
			k, _ := m.Load(tt.args.key)

			if (k == tt.args.value) != tt.want {
				t.Errorf("result = %v, want %v", result, tt.want)
			}
		})
	}
}

func Test_EvilScore(t *testing.T) {
	type args struct {
		ChainCoord *common.Coordinate
		Config     *Config
	}
	tests := []struct {
		name string
		args args
		want uint16
	}{
		{
			name: "string",
			args: args{
				ChainCoord: &common.Coordinate{},
				Config: &Config{
					Network:   "mock:evilscore",
					Port:      3005,
					StorePath: "./test/evilscore/",
				},
			},
			want: 10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pl, _ := NewPConnList(tt.args.Config.StorePath)
			pl.Store(PhysicalConnectionInfo{
				Addr:      "test",
				EvilScore: 10,
			})

			pi, err := pl.Get("test")
			if err != nil {
				t.Errorf("err = %v", err)
			}
			if pi.EvilScore != tt.want {
				t.Errorf("pi.EvilScore = %v, want %v", pi.EvilScore, tt.want)
			}

			pi.EvilScore *= tt.want
			pl.Store(pi)
			pi, err = pl.Get("test")
			if err != nil {
				t.Errorf("err = %v", err)
			}
			if pi.EvilScore != tt.want*tt.want {
				t.Errorf("pi.EvilScore = %v, want %v", pi.EvilScore, tt.want)
			}
		})
	}
}

func Test_router_UpdateEvilScore(t *testing.T) {
	type args struct {
		ChainCoord *common.Coordinate
		Config1    *Config
		Config2    *Config
	}
	tests := []struct {
		name    string
		args    args
		wantErr error
	}{
		{
			name: "string",
			args: args{
				ChainCoord: &common.Coordinate{},
				Config1: &Config{
					Network:   "mock:evilscore1",
					Port:      3004,
					StorePath: "./test/evilscore1/",
				},
				Config2: &Config{
					Network:   "mock:evilscore2",
					Port:      3004,
					StorePath: "./test/evilscore2/",
				},
			},
			wantErr: ErrDoNotRequestToEvelNode,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r1, _ := NewRouter(tt.args.Config1)
			r2, _ := NewRouter(tt.args.Config2)

			r1.AddListen(tt.args.ChainCoord)
			r2.AddListen(tt.args.ChainCoord)

			r2.Request("evilscore1:3004", tt.args.ChainCoord)

			wg := sync.WaitGroup{}
			wg.Add(2)

			var readConn net.Conn
			go func() {
				conn, _, _ := r1.Accept(tt.args.ChainCoord)
				readConn = conn
				wg.Done()
			}()
			go func() {
				r2.Accept(tt.args.ChainCoord)
				wg.Done()
			}()
			wg.Wait()

			r1.UpdateEvilScore(readConn.RemoteAddr().String(), 1000)
			readConn.Close()

			time.Sleep(time.Second)

			err := r1.Request("evilscore2:3004", tt.args.ChainCoord)

			if err != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
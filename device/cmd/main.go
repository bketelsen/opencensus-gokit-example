package main

import (
	// stdlib
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	// external
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/kit/sd/etcd"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/oklog/run"
	uuid "github.com/satori/go.uuid"
	"google.golang.org/grpc"

	// project
	"github.com/basvanbeek/opencensus-gokit-example"
	"github.com/basvanbeek/opencensus-gokit-example/device"
	"github.com/basvanbeek/opencensus-gokit-example/device/database/sqlite"
	"github.com/basvanbeek/opencensus-gokit-example/device/implementation"
	"github.com/basvanbeek/opencensus-gokit-example/device/transport"
	"github.com/basvanbeek/opencensus-gokit-example/device/transport/grpc"
	"github.com/basvanbeek/opencensus-gokit-example/device/transport/grpc/pb"
	svchttp "github.com/basvanbeek/opencensus-gokit-example/device/transport/http"
)

const (
	serviceName = "Device"
)

func main() {
	var (
		err      error
		instance = uuid.Must(uuid.NewV4())
	)

	// initialize our structured logger for the service
	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = log.NewSyncLogger(logger)
		logger = level.NewFilter(logger, level.AllowDebug())
		logger = log.With(logger,
			"svc", "Device",
			"instance", instance,
			"ts", log.DefaultTimestampUTC,
			"clr", log.DefaultCaller,
		)
	}

	level.Info(logger).Log("msg", "service started")
	defer level.Info(logger).Log("msg", "service ended")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create our etcd client for Service Discovery
	//
	// we could have used the v3 client but then we must vendor or suffer the
	// following issue originating from gRPC init:
	// panic: http: multiple registrations for /debug/requests
	var sdc etcd.Client
	{
		// create our Go kit etcd client
		sdc, err = etcd.NewClient(
			ctx, []string{"http://localhost:2379"}, etcd.ClientOptions{},
		)
		if err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}
	}

	// Create our DB Connection Driver
	var db *sqlx.DB
	{
		db, err = sqlx.Open("sqlite3", "testfile.db")
		if err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}

		// make sure the DB is in WAL mode
		if _, err = db.Exec(`PRAGMA journal_mode=wal`); err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}
	}

	// Create our Device Service
	var svc device.Service
	{
		repository, err := sqlite.New(db, logger)
		if err != nil {
			level.Error(logger).Log("exit", err)
			os.Exit(-1)
		}
		svc = implementation.NewService(repository, logger)
		// add service level middlewares here
	}

	// Create our Go kit endpoints for the Device Service
	var endpoints transport.Endpoints
	{
		endpoints = transport.MakeEndpoints(svc)
		// add endpoint level middlewares here
	}

	// find our host IP to advertise
	bindIP, err := ocgokitexample.HostIP()
	if err != nil {
		level.Error(logger).Log("exit", err)
		os.Exit(-1)
	}

	// run.Group manages our goroutine lifecycles
	// see: https://www.youtube.com/watch?v=LHe1Cb_Ud_M&t=15m45s
	var g run.Group
	{
		// set-up our grpc transport
		var (
			service      = svcgrpc.NewGRPCServer(endpoints, logger)
			listener, _  = net.Listen("tcp", bindIP+":0") // dynamic port assignment
			svcInstance  = "/services/" + serviceName + "/grpc/" + instance.String()
			addr         = listener.Addr().String()
			serviceEntry = etcd.Service{Key: svcInstance + addr, Value: addr}
			registrar    = etcd.NewRegistrar(sdc, serviceEntry, logger)
			grpcServer   = grpc.NewServer()
		)
		pb.RegisterDeviceServer(grpcServer, service)

		g.Add(func() error {
			registrar.Register()
			return grpcServer.Serve(listener)
		}, func(error) {
			registrar.Deregister()
			listener.Close()
		})
	}
	{
		// set-up our http transport
		var (
			service      = svchttp.NewHTTPHandler(endpoints)
			listener, _  = net.Listen("tcp", bindIP+":0") // dynamic port assignment
			svcInstance  = "/services/" + serviceName + "/http/" + instance.String()
			localAddr    = listener.Addr().String()
			serviceEntry = etcd.Service{Key: svcInstance, Value: localAddr}
			registrar    = etcd.NewRegistrar(sdc, serviceEntry, logger)
		)

		g.Add(func() error {
			return http.Serve(listener, service)
		}, func(error) {
			registrar.Deregister()
			listener.Close()
		})
	}
	{
		// set-up our signal handler
		var (
			cancelInterrupt = make(chan struct{})
			c               = make(chan os.Signal, 2)
		)
		defer close(c)

		g.Add(func() error {
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
			select {
			case sig := <-c:
				return fmt.Errorf("received signal %s", sig)
			case <-cancelInterrupt:
				return nil
			}
		}, func(error) {
			close(cancelInterrupt)
		})
	}

	// spawn our goroutines and wait for shutdown
	level.Error(logger).Log("exit", g.Run())
}
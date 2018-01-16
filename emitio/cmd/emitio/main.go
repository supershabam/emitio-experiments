package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/dgraph-io/badger"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/supershabam/emitio/emitio"
	"github.com/supershabam/emitio/emitio/pb"
	"github.com/supershabam/emitio/emitio/pkg/ingresses"
	"github.com/supershabam/emitio/emitio/pkg/listeners"
)

func main() {
	// --origin pod=$(pod_name)
	// --origin namespace=$(k8s_namespace)
	// --origin datacenter=nyc2
	// --ingress tail:///var/log/message
	// --ingress syslog+udp://0.0.0.0:514/?my_tag=value
	// --ingress ndjson+stdin:///
	// --ingress statsd+udp://0.0.0.0:9001/?region=nyc2#application=something
	// --ingress tail:///var/log/mongodb/mongodb.log#hint=mongodb-v3.18
	// --ingress opentracing+udp://0.0.0.0:9002/
	// --forward https://ingress.emit.io/
	ctx, cancel := context.WithCancel(context.Background())
	logger, _ := zap.NewProduction()
	defer logger.Sync() // flushes buffer, if any
	sigch := make(chan os.Signal, 2)
	signal.Notify(sigch, os.Interrupt)
	go func() {
		<-sigch
		cancel()
		<-sigch
		os.Exit(1)
	}()
	opts := badger.DefaultOptions
	opts.Dir = "/tmp/emitio"
	opts.ValueDir = "/tmp/emitio"
	db, err := badger.Open(opts)
	if err != nil {
		logger.Fatal("badger open", zap.Error(err))
	}
	defer db.Close()
	i, err := ingresses.MakeIngress("udp://localhost:9008")
	if err != nil {
		panic(err)
	}
	s, err := emitio.NewServer(ctx,
		emitio.WithIngresses([]emitio.Ingresser{i}),
		emitio.WithDB(db),
	)
	if err != nil {
		panic(err)
	}
	dialer := func(ctx context.Context) (net.Conn, error) {
		var conn net.Conn
		d := net.Dialer{}
		operation := func() error {
			logger.Info("dialing")
			_conn, err := d.DialContext(ctx, "tcp", ":8080")
			if err != nil {
				return err
			}
			conn = _conn
			return nil
		}
		policy := backoff.NewExponentialBackOff()
		policy.Multiplier = 1.8
		policy.MaxInterval = 5 * time.Minute
		policy.MaxElapsedTime = 0 // allow looping forever until the context is cancelled
		err := backoff.Retry(operation, backoff.WithContext(policy, ctx))
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
	lis := listeners.NewReverse(dialer)
	grpcServer := grpc.NewServer()
	pb.RegisterEmitioServer(grpcServer, s)
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return s.Run(ctx)
	})
	eg.Go(func() error {
		go func() {
			<-ctx.Done()
			tctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			go func() {
				<-tctx.Done()
				grpcServer.Stop()
			}()
			grpcServer.GracefulStop()
		}()
		return grpcServer.Serve(lis)
	})
	err = eg.Wait()
	if err != nil {
		logger.Fatal("unrecoverable error", zap.Error(err))
	}
}

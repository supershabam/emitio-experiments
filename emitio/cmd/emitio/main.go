package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dgraph-io/badger"

	"github.com/supershabam/emitio/emitio"
	"github.com/supershabam/emitio/emitio/pb"
	"github.com/supershabam/emitio/emitio/pkg/ingresses"
	"google.golang.org/grpc"
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
	// --listen 0.0.0.0:8080
	ctx, cancel := context.WithCancel(context.Background())
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
		panic(err)
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
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}
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
	go func() {
		cc, err := grpc.Dial("localhost:8080", grpc.WithInsecure())
		if err != nil {
			panic(err)
		}
		client := pb.NewEmitioClient(cc)
		stream, err := client.ReadRows(ctx, &pb.ReadRowsRequest{})
		if err != nil {
			panic(err)
		}
		for {
			reply, err := stream.Recv()
			if err != nil {
				panic(err)
			}
			fmt.Printf("%+v\n", reply)
		}
	}()
	err = eg.Wait()
	if err != nil {
		panic(err)
	}
}

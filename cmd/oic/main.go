package main

import (
	"context"
	"github.com/wyw-ai/loom-oi/internal/oic"
	"github.com/wyw-ai/loom-oi/internal/servicecmd"
	"os"
)

type backend struct{ host *oic.Host }

func newBackend(path string) (servicecmd.Backend, error) {
	h, e := oic.New(path)
	if e != nil {
		return nil, e
	}
	return &backend{h}, nil
}
func (b *backend) Run(ctx context.Context) error   { return b.host.Run(ctx) }
func (b *backend) Close(ctx context.Context) error { return b.host.Close(ctx) }
func (b *backend) Status() any                     { return b.host.Status() }
func (b *backend) Apps() (any, error)              { return b.host.Apps(), nil }
func (b *backend) Load(path string) (any, error)   { return b.host.Load(path) }
func (b *backend) Unload(code string) error        { return b.host.Unload(code) }
func main()                                        { os.Exit(servicecmd.Main("oic", os.Args[1:], newBackend)) }

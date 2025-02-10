//go:build !darwin

package main

import (
	"context"
	"fmt"
)

func (hdl *serviceHandler) Stop(ctx context.Context) error  { return fmt.Errorf("TODO") }
func (hdl *serviceHandler) Start(ctx context.Context) error { return fmt.Errorf("TODO") }
func (hdl *serviceHandler) Uninstall() error                { return fmt.Errorf("TODO") }
func (hdl *serviceHandler) Install() error                  { return fmt.Errorf("TODO") }

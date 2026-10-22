//go:build !windows

package app

import (
	"context"
	"errors"
)

// 非 Windows 平台只保留接口，方便在 CI 上跑 `go vet ./...`。

var errUnsupported = errors.New("当前平台不支持剪贴板读写")

func GetClipboardText() (string, error) { return "", errUnsupported }
func SetClipboardText(string) error     { return errUnsupported }
func EnableUTF8Console()                {}

type ClipboardWatcher struct{ onClip func(string) }

func NewClipboardWatcher(onClip func(string)) *ClipboardWatcher {
	return &ClipboardWatcher{onClip: onClip}
}

func (w *ClipboardWatcher) Run(ctx context.Context) { <-ctx.Done() }
func (w *ClipboardWatcher) SetText(string) error    { return errUnsupported }

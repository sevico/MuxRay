//go:build !windows || !pickerprocess

package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func runPickerChild() (bool, int) { return false, 0 }

func openFileDialog(ctx context.Context) (string, error) {
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:   "选择 FLV 文件",
		Filters: []runtime.FileFilter{{DisplayName: "FLV 文件 (*.flv)", Pattern: "*.flv"}},
	})
}

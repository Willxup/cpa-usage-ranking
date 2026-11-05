package avatars

import (
	"embed"
	"io/fs"
)

// 仅嵌入已批准的目录与图片，来源说明不对外分发。
//
//go:embed assets/catalog.json assets/images/*.webp
var builtin embed.FS

// BuiltinFS returns the read-only, embedded production resource root.
func BuiltinFS() fs.FS {
	files, err := fs.Sub(builtin, "assets")
	if err != nil {
		panic(err) // 固定的嵌入目录，不取决于运行时输入。
	}
	return files
}

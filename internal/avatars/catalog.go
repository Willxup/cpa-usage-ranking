// Package avatars 加载服务端维护的静态头像目录，不生成、上传或分发图片。
package avatars

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// Avatar 的路径相对于传给 Load 的文件系统根，不是 URL。
type Avatar struct {
	ID   int64  `json:"avatar_id"`
	Path string `json:"path"`
}

// Catalog 加载后只读；编号来自目录本身，不从数组位置生成。
type Catalog struct {
	defaultID int64
	entries   []Avatar
	byID      map[int64]Avatar
}

// Load 读取根目录的 catalog.json 并检查全部资源，不提供隐式目录或默认头像。
// files 应由调用方约束在可信资源根内，例如 embed.FS 或 os.Root.FS()。
func Load(files fs.FS) (*Catalog, error) {
	data, err := fs.ReadFile(files, "catalog.json")
	if err != nil {
		return nil, fmt.Errorf("read avatar catalog: %w", err)
	}
	var input struct {
		DefaultID int64    `json:"default_avatar_id"`
		Avatars   []Avatar `json:"avatars"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("decode avatar catalog: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("decode avatar catalog: expected exactly one JSON object")
	}
	if len(input.Avatars) == 0 {
		return nil, fmt.Errorf("avatar catalog: avatars must not be empty")
	}

	catalog := &Catalog{defaultID: input.DefaultID, entries: input.Avatars, byID: make(map[int64]Avatar, len(input.Avatars))}
	for _, avatar := range input.Avatars {
		if avatar.ID <= 0 {
			return nil, fmt.Errorf("avatar catalog: avatar_id must be positive, got %d", avatar.ID)
		}
		if _, exists := catalog.byID[avatar.ID]; exists {
			return nil, fmt.Errorf("avatar catalog: duplicate avatar_id %d", avatar.ID)
		}
		// 不清理不合法路径，也不把路径解释为远程地址；静态分发沿用同一资源根。
		if !fs.ValidPath(avatar.Path) || avatar.Path == "." || strings.ContainsAny(avatar.Path, `\:?#`) {
			return nil, fmt.Errorf("avatar catalog: avatar_id %d has invalid resource path %q", avatar.ID, avatar.Path)
		}
		info, err := fs.Stat(files, avatar.Path)
		if err != nil {
			return nil, fmt.Errorf("avatar catalog: avatar_id %d resource %q: %w", avatar.ID, avatar.Path, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("avatar catalog: avatar_id %d resource must be a regular file: %q", avatar.ID, avatar.Path)
		}
		catalog.byID[avatar.ID] = avatar
	}
	if _, exists := catalog.byID[catalog.defaultID]; !exists {
		return nil, fmt.Errorf("avatar catalog: default_avatar_id %d is not in the catalog", catalog.defaultID)
	}
	return catalog, nil
}

// DefaultID 用于未提交头像编号的情形；显式非法编号不能静默回退。
func (c *Catalog) DefaultID() int64 { return c.defaultID }

func (c *Catalog) Lookup(id int64) (Avatar, bool) {
	avatar, ok := c.byID[id]
	return avatar, ok
}

// List 保留目录顺序并返回副本，调用方不能改写已校验的目录。
func (c *Catalog) List() []Avatar { return append([]Avatar(nil), c.entries...) }

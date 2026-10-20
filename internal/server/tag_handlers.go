// Package server - 标签凭据管理 handler。
//
// 独立的「邮箱/密码 + 项目标签」记录,与 iCloud 账号体系无关:
// 不触碰 accounts.json,也不影响任何 Hide My Email 流程。
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/tagstore"
)

// listTagsHandler 处理 GET /api/tags,返回全部记录与已用标签。
func (s *Server) listTagsHandler(c *gin.Context) {
	entries := s.tags.List()
	ok(c, gin.H{
		"count":   len(entries),
		"tags":    s.tags.Tags(),
		"entries": entries,
	})
}

// createTagHandler 处理 POST /api/tags。
func (s *Server) createTagHandler(c *gin.Context) {
	var req tagstore.Input
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	entry, err := s.tags.Create(req)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, entry)
}

// updateTagHandler 处理 PUT /api/tags/:id。
func (s *Server) updateTagHandler(c *gin.Context) {
	id := c.Param("id")
	if id == "" || len(id) > 128 {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误: id 无效")
		return
	}
	var req tagstore.Input
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	entry, err := s.tags.Update(id, req)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, entry)
}

// deleteTagHandler 处理 DELETE /api/tags/:id。
func (s *Server) deleteTagHandler(c *gin.Context) {
	id := c.Param("id")
	if id == "" || len(id) > 128 {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误: id 无效")
		return
	}
	removed, err := s.tags.Delete(id)
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "删除失败")
		return
	}
	if !removed {
		failCode(c, http.StatusNotFound, "TAG_NOT_FOUND", "记录不存在")
		return
	}
	ok(c, gin.H{"id": id})
}

// importTagsHandler 处理 POST /api/tags/import,批量导入并给所有记录打上选定标签。
func (s *Server) importTagsHandler(c *gin.Context) {
	var req tagstore.ImportInput
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	result, err := s.tags.Import(req)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, result)
}

// renameTagLabelHandler 处理 PUT /api/tag-labels/rename,全局重命名一个标签。
func (s *Server) renameTagLabelHandler(c *gin.Context) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	changed, err := s.tags.RenameTag(req.From, req.To)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, gin.H{
		"changed": changed,
		"from":    strings.TrimSpace(req.From),
		"to":      strings.TrimSpace(req.To),
	})
}

// deleteTagLabelHandler 处理 DELETE /api/tag-labels/:name,从所有记录中移除标签。
func (s *Server) deleteTagLabelHandler(c *gin.Context) {
	name := c.Param("name")
	if name == "" || len(name) > 256 {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误: 标签无效")
		return
	}
	changed, err := s.tags.DeleteTag(name)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, gin.H{"changed": changed, "tag": name})
}

// exportTagsHandler 处理 GET /api/tags/export,返回可迁移的备份文档。
func (s *Server) exportTagsHandler(c *gin.Context) {
	ok(c, s.tags.Export())
}

// restoreTagsHandler 处理 POST /api/tags/restore,从备份文档导入记录。
func (s *Server) restoreTagsHandler(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil || len(raw) == 0 {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	in, err := parseRestorePayload(raw)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	result, err := s.tags.Restore(in)
	if err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	ok(c, result)
}

// parseRestorePayload 兼容三种备份载体:备份对象、API 响应包裹、纯记录数组。
func parseRestorePayload(raw []byte) (tagstore.RestoreInput, error) {
	var direct tagstore.RestoreInput
	if err := json.Unmarshal(raw, &direct); err == nil && len(direct.Entries) > 0 {
		return direct, nil
	}

	var wrapped struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Data) > 0 {
		var in tagstore.RestoreInput
		if err := json.Unmarshal(wrapped.Data, &in); err == nil && len(in.Entries) > 0 {
			if in.Mode == "" {
				in.Mode = direct.Mode
			}
			return in, nil
		}
	}

	var entries []tagstore.Entry
	if err := json.Unmarshal(raw, &entries); err == nil && len(entries) > 0 {
		return tagstore.RestoreInput{Entries: entries, Mode: direct.Mode}, nil
	}

	return tagstore.RestoreInput{}, fmt.Errorf("无法解析备份文件")
}

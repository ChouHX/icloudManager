// Package server - 标签凭据管理 handler。
//
// 独立的「邮箱/密码 + 项目标签」记录,与 iCloud 账号体系无关:
// 不触碰 accounts.json,也不影响任何 Hide My Email 流程。
package server

import (
	"net/http"

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

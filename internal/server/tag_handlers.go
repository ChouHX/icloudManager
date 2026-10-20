// Package server - 标签凭据管理 handler。
//
// 独立的「邮箱/密码 + 项目标签」记录,与 iCloud 账号体系无关:
// 不触碰 accounts.json,也不影响任何 Hide My Email 流程。
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/tagstore"
)

// listTagsHandler 处理 GET /api/tags,返回分页记录、标签统计与汇总卡片数据。
//
// 查询参数:
//
//	page       页码,默认 1
//	page_size  每页条数,默认 20,最大 200
//	search     关键字(邮箱/网站/备注/标签)
//	tags       标签筛选,逗号分隔,任一命中即可(OR)
func (s *Server) listTagsHandler(c *gin.Context) {
	page := parsePositiveInt(c.Query("page"), 1)
	pageSize := parsePositiveInt(c.Query("page_size"), 20)
	from := c.Query("created_from")
	to := c.Query("created_to")
	if err := validateTimeRange(from, to); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}

	res, err := s.tags.ListPage(tagstore.ListQuery{
		Page:        page,
		PageSize:    pageSize,
		Search:      c.Query("search"),
		Tags:        parseTagFilters(c.QueryArray("tags")),
		Site:        c.Query("site"),
		CreatedFrom: from,
		CreatedTo:   to,
	})
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取标签记录失败")
		return
	}
	counts, err := s.tags.TagCounts()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取标签统计失败")
		return
	}
	stats, err := s.tags.Stats()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取标签统计失败")
		return
	}
	sites, err := s.tags.Sites()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取网站列表失败")
		return
	}
	tags := make([]string, len(counts))
	for i, usage := range counts {
		tags[i] = usage.Tag
	}

	ok(c, gin.H{
		"count":       res.Total,
		"page":        res.Page,
		"page_size":   res.PageSize,
		"total_pages": (res.Total + res.PageSize - 1) / res.PageSize,
		"tags":        tags,
		"tag_counts":  counts,
		"sites":       sites,
		"stats":       stats,
		"entries":     res.Entries,
	})
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// parseTagFilters 兼容 tags=a,b 与 tags=a&tags=b 两种写法。
func parseTagFilters(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
	}
	return out
}

// batchTagsHandler 处理 POST /api/tags/batch,按 ID 或筛选条件批量修改标签。
func (s *Server) batchTagsHandler(c *gin.Context) {
	var req struct {
		IDs    []string `json:"ids"`
		Filter struct {
			Search      string   `json:"search"`
			Tags        []string `json:"tags"`
			Site        string   `json:"site"`
			CreatedFrom string   `json:"created_from"`
			CreatedTo   string   `json:"created_to"`
		} `json:"filter"`
		Action string   `json:"action"`
		Tags   []string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", "参数错误")
		return
	}
	if err := validateTimeRange(req.Filter.CreatedFrom, req.Filter.CreatedTo); err != nil {
		failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}

	result, err := s.tags.BatchTag(tagstore.BatchInput{
		IDs: req.IDs,
		Filter: tagstore.ListQuery{
			Search:      req.Filter.Search,
			Tags:        req.Filter.Tags,
			Site:        req.Filter.Site,
			CreatedFrom: req.Filter.CreatedFrom,
			CreatedTo:   req.Filter.CreatedTo,
		},
		Action: req.Action,
		Tags:   req.Tags,
	})
	if err != nil {
		var invalid *tagstore.BatchValidationError
		if errors.As(err, &invalid) {
			failCode(c, http.StatusBadRequest, "VALIDATION_ERROR", invalid.Message)
			return
		}
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "批量修改标签失败")
		return
	}
	ok(c, result)
}

// validateTimeRange 校验 RFC3339 时间参数,并确保起始不晚于结束。
func validateTimeRange(from, to string) error {
	var fromTime, toTime time.Time
	if from != "" {
		parsed, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return fmt.Errorf("created_from 需要 RFC3339 时间格式")
		}
		fromTime = parsed
	}
	if to != "" {
		parsed, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return fmt.Errorf("created_to 需要 RFC3339 时间格式")
		}
		toTime = parsed
	}
	if from != "" && to != "" && fromTime.After(toTime) {
		return fmt.Errorf("created_from 不能晚于 created_to")
	}
	return nil
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
	// 万级导入时逐条回传会显著撑大响应,前端也只用统计信息。
	result.Entries = nil
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
	backup, err := s.tags.Export()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "导出备份失败")
		return
	}
	ok(c, backup)
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

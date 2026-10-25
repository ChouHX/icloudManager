// Package server - 数据看板 handler。
//
// 聚合 iCloud 账号的别名数量与标签凭据的网站/标签分布,只读、不触发上游请求。
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"icloud-hme/internal/account"
	"icloud-hme/internal/tagstore"
)

// dashboardAccountStats 是看板中的 iCloud 账号汇总。
type dashboardAccountStats struct {
	Total       int               `json:"total"`
	AliasTotal  int               `json:"alias_total"`
	AliasActive int               `json:"alias_active"`
	Items       []account.Summary `json:"items"`
}

// dashboardTagStats 是看板中的标签凭据汇总与分布。
type dashboardTagStats struct {
	Total     int                     `json:"total"`
	Tagged    int                     `json:"tagged"`
	Untagged  int                     `json:"untagged"`
	TagKinds  int                     `json:"tag_kinds"`
	SiteKinds int                     `json:"site_kinds"`
	BySite    []tagstore.SiteUsage    `json:"by_site"`
	ByTag     []tagstore.TagUsage     `json:"by_tag"`
	BySiteTag []tagstore.SiteTagUsage `json:"by_site_tag"`
}

// dashboardResponse 是 GET /api/dashboard 的响应体。
type dashboardResponse struct {
	Accounts dashboardAccountStats `json:"accounts"`
	Tags     dashboardTagStats     `json:"tags"`
}

// dashboardHandler 处理 GET /api/dashboard,返回账号与标签的聚合统计。
func (s *Server) dashboardHandler(c *gin.Context) {
	accounts := s.be.ListAccounts()
	accStats := dashboardAccountStats{Total: len(accounts), Items: accounts}
	for _, a := range accounts {
		accStats.AliasTotal += a.AliasTotal
		accStats.AliasActive += a.AliasActive
	}

	stats, err := s.tags.Stats()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取标签统计失败")
		return
	}
	bySite, err := s.tags.SiteCounts()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取网站统计失败")
		return
	}
	byTag, err := s.tags.TagCounts()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取标签统计失败")
		return
	}
	bySiteTag, err := s.tags.SiteTagCounts()
	if err != nil {
		failCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "读取网站标签统计失败")
		return
	}

	ok(c, dashboardResponse{
		Accounts: accStats,
		Tags: dashboardTagStats{
			Total:     stats.Total,
			Tagged:    stats.Total - stats.Untagged,
			Untagged:  stats.Untagged,
			TagKinds:  stats.Tags,
			SiteKinds: stats.Sites,
			BySite:    bySite,
			ByTag:     byTag,
			BySiteTag: bySiteTag,
		},
	})
}

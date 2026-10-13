package co

import (
	"context"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/collector"
	"github.com/nekoimi/scrapio/internal/config"
	"github.com/nekoimi/scrapio/internal/drission_rod"
	"github.com/nekoimi/scrapio/internal/repo/v22_credential_repo"
)

// CheckAvailability performs capability/credential checks outside database write transactions.
func CheckAvailability(ctx context.Context, owner int64, d collector.Definition, input map[string]any, browser *drission_rod.DrissionRod, cfg *config.Config) error {
	r, e := collector.Prepare(d, input)
	if e != nil {
		return e
	}
	if d.Fetch.Kind == "browser" {
		if browser == nil {
			return collector.Fault("BROWSER_UNAVAILABLE", "publication", "浏览器服务未配置")
		}
		if e = browser.ProbeEditorIsolation(ctx); e != nil {
			return browserError(e, "publication")
		}
		_, _, e = v22_credential_repo.Browser(ctx, owner, r.URL, d.Fetch.CredentialRef)
	} else {
		var httpConfig *config.HTTPEntryConfig
		if cfg != nil {
			httpConfig = cfg.HTTPEntry
		}
		_, _, _, e = capture.Credential(httpConfig, owner, r.URL, d.Fetch.CredentialRef)
	}
	if e != nil {
		return collector.Fault("CREDENTIAL_UNAVAILABLE", "publication", "凭据已失效或超出访问范围，请重新测试")
	}
	return nil
}

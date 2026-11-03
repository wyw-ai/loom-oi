// Package limits holds the B4 §3 initial test configuration. Values are
// defaults for P1, not SLOs or capacity promises; deployments override any
// of them through the "limits" object of the OIS or OIC configuration.
package limits

import (
	"fmt"
	"time"
)

// Limits uses milliseconds for durations so configuration files stay plain
// JSON numbers.
type Limits struct {
	NOIS             int   `json:"n_ois"`
	NAccess          int   `json:"n_access"`
	NSubject         int   `json:"n_subject"`
	MaxLocalChannels int   `json:"max_local_channels"`
	DataMax          int   `json:"data_max"`
	HTTPMax          int64 `json:"http_max"`
	CtrlMax          int64 `json:"ctrl_max"`
	PageMax          int   `json:"page_max"`
	PageDefault      int   `json:"page_default"`
	MaxExposures     int   `json:"max_exposures"`
	DescMax          int   `json:"desc_max"`
	PrepareMS        int64 `json:"prepare_ms"`
	DialMS           int64 `json:"dial_ms"`
	CloseMS          int64 `json:"close_ms"`
	RetainMS         int64 `json:"retain_ms"`
	HeartbeatMS      int64 `json:"heartbeat_ms"`
	OfflineMisses    int   `json:"offline_misses"`
	LicenseTTLMS     int64 `json:"license_ttl_ms"`
	LicenseMarginMS  int64 `json:"license_margin_ms"`
	PingMS           int64 `json:"ping_ms"`
	IdleMS           int64 `json:"idle_ms"`
	TokenTTLMS       int64 `json:"token_ttl_ms"`
	TokenTTLMaxMS    int64 `json:"token_ttl_max_ms"`
	WriteQueue       int   `json:"write_queue"`
	SQLiteBusyMS     int64 `json:"sqlite_busy_ms"`
	StoreProbeMS     int64 `json:"store_probe_ms"`
}

const day = 24 * time.Hour

// Defaults returns the P1 defaults of B4 §3.
func Defaults() Limits {
	return Limits{
		NOIS: 64, NAccess: 32, NSubject: 16, MaxLocalChannels: 32,
		DataMax: 32 << 10, HTTPMax: 64 << 10, CtrlMax: 256 << 10,
		PageMax: 100, PageDefault: 50, MaxExposures: 128, DescMax: 2 << 10,
		PrepareMS: 10_000, DialMS: 5_000, CloseMS: 5_000, RetainMS: 60_000,
		HeartbeatMS: 10_000, OfflineMisses: 3, LicenseTTLMS: 30_000, LicenseMarginMS: 2_000,
		PingMS: 15_000, IdleMS: 600_000,
		TokenTTLMS: int64(30 * day / time.Millisecond), TokenTTLMaxMS: int64(90 * day / time.Millisecond),
		WriteQueue: 64, SQLiteBusyMS: 5_000, StoreProbeMS: 5_000,
	}
}

// Validate rejects values that would make a component unsafe or useless.
func (l Limits) Validate() error {
	positive := map[string]int64{
		"n_ois": int64(l.NOIS), "n_access": int64(l.NAccess), "n_subject": int64(l.NSubject),
		"max_local_channels": int64(l.MaxLocalChannels), "data_max": int64(l.DataMax),
		"http_max": l.HTTPMax, "ctrl_max": l.CtrlMax, "page_max": int64(l.PageMax),
		"page_default": int64(l.PageDefault), "max_exposures": int64(l.MaxExposures),
		"desc_max": int64(l.DescMax), "prepare_ms": l.PrepareMS, "dial_ms": l.DialMS,
		"close_ms": l.CloseMS, "retain_ms": l.RetainMS, "heartbeat_ms": l.HeartbeatMS,
		"offline_misses": int64(l.OfflineMisses), "license_ttl_ms": l.LicenseTTLMS,
		"ping_ms": l.PingMS, "idle_ms": l.IdleMS, "token_ttl_ms": l.TokenTTLMS,
		"token_ttl_max_ms": l.TokenTTLMaxMS, "write_queue": int64(l.WriteQueue),
		"sqlite_busy_ms": l.SQLiteBusyMS, "store_probe_ms": l.StoreProbeMS,
	}
	for name, v := range positive {
		if v <= 0 {
			return fmt.Errorf("limits.%s must be positive", name)
		}
	}
	switch {
	case l.LicenseMarginMS < 0 || l.LicenseMarginMS >= l.LicenseTTLMS:
		return fmt.Errorf("limits.license_margin_ms must be in [0, license_ttl_ms)")
	case l.PageDefault > l.PageMax:
		return fmt.Errorf("limits.page_default exceeds page_max")
	case l.TokenTTLMS > l.TokenTTLMaxMS:
		return fmt.Errorf("limits.token_ttl_ms exceeds token_ttl_max_ms")
	case l.DataMax > 1<<20:
		return fmt.Errorf("limits.data_max exceeds 1 MiB")
	}
	return nil
}

// D converts a millisecond limit to a duration.
func D(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }

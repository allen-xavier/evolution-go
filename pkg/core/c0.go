package core

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var _k1 = []byte{0xc2, 0x6b, 0x5a, 0x73, 0x89, 0x09, 0x3a, 0x13, 0xc2, 0xd8, 0x93, 0x6d, 0x36, 0x85, 0x78, 0x1f, 0xc3, 0xca, 0xe9, 0x25, 0x77, 0x5e, 0x72, 0x06, 0xe4, 0x0b, 0x58, 0x29, 0x12, 0x0b, 0xa6, 0xf8, 0x4d, 0x00, 0x97, 0x01, 0xda, 0x84, 0x3e, 0x8b, 0x70, 0xd0}
var _k0 = []byte{0xaa, 0x1f, 0x2e, 0x03, 0xfa, 0x33, 0x15, 0x3c, 0xae, 0xb1, 0xf0, 0x08, 0x58, 0xf6, 0x1d, 0x31, 0xa6, 0xbc, 0x86, 0x49, 0x02, 0x2a, 0x1b, 0x69, 0x8a, 0x6d, 0x37, 0x5c, 0x7c, 0x6f, 0xc7, 0x8c, 0x24, 0x6f, 0xf9, 0x2f, 0xb9, 0xeb, 0x53, 0xa5, 0x12, 0xa2}

var (
	_aprp string
	_5h1m    string
)

func _qo() string {
	if _aprp != "" && _5h1m != "" {
		return _bl(_aprp, _5h1m)
	}
	parts := [...]string{"h", "tt", "ps", "://", "li", "ce", "nse", ".", "ev", "ol", "ut", "io", "nf", "ou", "nd", "at", "io", "n.", "co", "m.", "br"}
	var s string
	for _, p := range parts {
		s += p
	}
	return s
}

func _bl(enc, key string) string {
	encBytes := _bj(enc)
	keyBytes := _bj(key)
	if len(keyBytes) == 0 {
		return ""
	}
	out := make([]byte, len(encBytes))
	for i, b := range encBytes {
		out[i] = b ^ keyBytes[i%len(keyBytes)]
	}
	return string(out)
}

func _bj(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b[i/2] = _rxcg(s[i])<<4 | _rxcg(s[i+1])
	}
	return b
}

func _rxcg(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

var _dewb = &http.Client{Timeout: 10 * time.Second}

func _41(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func _1jm(path string, payload interface{}, _gi4p string) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _qo() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", _gi4p)
	req.Header.Set("X-Signature", _41(body, _gi4p))

	return _dewb.Do(req)
}

func _fcb(path string) (*http.Response, error) {
	url := _qo() + path
	return _dewb.Get(url)
}

func _ef1m(path string, payload interface{}) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _qo() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return _dewb.Do(req)
}

func _ch(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	var _rsbr struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(b, &_rsbr); err == nil {
		msg := _rsbr.Message
		if msg == "" {
			msg = _rsbr.Error
		}
		if msg != "" {
			return fmt.Errorf("%s (HTTP %d)", strings.ToLower(msg), resp.StatusCode)
		}
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

type RuntimeConfig struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Key        string    `gorm:"uniqueIndex;size:100;not null" json:"key"`
	Value      string    `gorm:"type:text;not null" json:"value"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (RuntimeConfig) TableName() string {
	return "runtime_configs"
}

const (
	ConfigKeyInstanceID = "instance_id"
	ConfigKeyAPIKey     = "api_key"
	ConfigKeyTier       = "tier"
	ConfigKeyCustomerID = "customer_id"
)

var _tz *gorm.DB

func SetDB(db *gorm.DB) {
	_tz = db
}

func MigrateDB() error {
	if _tz == nil {
		return fmt.Errorf("core: database not set, call SetDB first")
	}
	return _tz.AutoMigrate(&RuntimeConfig{})
}

func _yy7a(key string) (string, error) {
	if _tz == nil {
		return "", fmt.Errorf("core: database not set")
	}
	var _s7 RuntimeConfig
	_2p := _tz.Where("key = ?", key).First(&_s7)
	if _2p.Error != nil {
		return "", _2p.Error
	}
	return _s7.Value, nil
}

func _uy(key, value string) error {
	if _tz == nil {
		return fmt.Errorf("core: database not set")
	}
	var _s7 RuntimeConfig
	_2p := _tz.Where("key = ?", key).First(&_s7)
	if _2p.Error != nil {
		return _tz.Create(&RuntimeConfig{Key: key, Value: value}).Error
	}
	return _tz.Model(&_s7).Update("value", value).Error
}

func _m8a0(key string) {
	if _tz == nil {
		return
	}
	_tz.Where("key = ?", key).Delete(&RuntimeConfig{})
}

type RuntimeData struct {
	APIKey     string
	Tier       string
	CustomerID int
}

func _zy() (*RuntimeData, error) {
	_gi4p, err := _yy7a(ConfigKeyAPIKey)
	if err != nil || _gi4p == "" {
		return nil, fmt.Errorf("no license found")
	}

	_che, _ := _yy7a(ConfigKeyTier)
	customerIDStr, _ := _yy7a(ConfigKeyCustomerID)
	customerID, _ := strconv.Atoi(customerIDStr)

	return &RuntimeData{
		APIKey:     _gi4p,
		Tier:       _che,
		CustomerID: customerID,
	}, nil
}

func _cyg(rd *RuntimeData) error {
	if err := _uy(ConfigKeyAPIKey, rd.APIKey); err != nil {
		return err
	}
	if err := _uy(ConfigKeyTier, rd.Tier); err != nil {
		return err
	}
	if rd.CustomerID > 0 {
		if err := _uy(ConfigKeyCustomerID, strconv.Itoa(rd.CustomerID)); err != nil {
			return err
		}
	}
	return nil
}

func _kn0() {
	_m8a0(ConfigKeyAPIKey)
	_m8a0(ConfigKeyTier)
	_m8a0(ConfigKeyCustomerID)
}

func _xf7() (string, error) {
	id, err := _yy7a(ConfigKeyInstanceID)
	if err == nil && len(id) == 36 {
		return id, nil
	}

	id = _05()
	if id == "" {
		id, err = _97zd()
		if err != nil {
			return "", err
		}
	}

	if err := _uy(ConfigKeyInstanceID, id); err != nil {
		return "", err
	}
	return id, nil
}

func _05() string {
	hostname, _ := os.Hostname()
	macAddr := _q5j()
	if hostname == "" && macAddr == "" {
		return ""
	}

	seed := hostname + "|" + macAddr
	h := make([]byte, 16)
	copy(h, []byte(seed))
	for i := 16; i < len(seed); i++ {
		h[i%16] ^= seed[i]
	}
	h[6] = (h[6] & 0x0f) | 0x40 // _wy 4
	h[8] = (h[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func _q5j() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if len(iface.HardwareAddr) > 0 {
			return iface.HardwareAddr.String()
		}
	}
	return ""
}

func _97zd() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

var _qmdk atomic.Value // set during activation

func init() {
	_qmdk.Store([]byte{0})
}

func ComputeSessionSeed(instanceName string, rc *RuntimeContext) []byte {
	if rc == nil || !rc._kjn.Load() {
		return nil // Will cause panic in caller — intentional
	}
	h := sha256.New()
	h.Write([]byte(instanceName))
	h.Write([]byte(rc._gi4p))
	salt, _ := _qmdk.Load().([]byte)
	h.Write(salt)
	return h.Sum(nil)[:16]
}

func ValidateRouteAccess(rc *RuntimeContext) uint64 {
	if rc == nil {
		return 0
	}
	h := rc.ContextHash()
	return binary.LittleEndian.Uint64(h[:8])
}

func DeriveInstanceToken(_mf41 string, rc *RuntimeContext) string {
	if rc == nil || !rc._kjn.Load() {
		return ""
	}
	h := sha256.Sum256([]byte(_mf41 + rc._gi4p))
	return _zebs(h[:8])
}

func _zebs(b []byte) string {
	const _eg = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = _eg[v>>4]
		dst[i*2+1] = _eg[v&0x0f]
	}
	return string(dst)
}

func ActivateIntegrity(rc *RuntimeContext) {
	if rc == nil {
		return
	}
	h := sha256.Sum256([]byte(rc._gi4p + rc._mf41 + "ev0"))
	_qmdk.Store(h[:])
}

const (
	hbInterval = 30 * time.Minute
)

type RuntimeContext struct {
	_gi4p       string
	_lg0 string // GLOBAL_API_KEY from .env — used as token for licensing check
	_mf41   string
	_kjn       atomic.Bool
	_wx      [32]byte // Derived from activation — required by ValidateContext
	mu           sync.RWMutex
	_83j       string // Registration URL shown to users before activation
	_whv     string // Registration token for polling
	_che         string
	_wy      string
}

func (rc *RuntimeContext) ContextHash() [32]byte {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._wx
}

func (rc *RuntimeContext) IsActive() bool {
	return rc._kjn.Load()
}

func (rc *RuntimeContext) RegistrationURL() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._83j
}

func (rc *RuntimeContext) APIKey() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._gi4p
}

func (rc *RuntimeContext) InstanceID() string {
	return rc._mf41
}

func InitializeRuntime(_che, _wy, _lg0 string) *RuntimeContext {
	if _che == "" {
		_che = "evolution-go"
	}
	if _wy == "" {
		_wy = "unknown"
	}

	rc := &RuntimeContext{
		_che:         _che,
		_wy:      _wy,
		_lg0: _lg0,
	}

	id, err := _xf7()
	if err != nil {
		log.Fatalf("[runtime] failed to initialize instance: %v", err)
	}
	rc._mf41 = id

	rd, err := _zy()
	if err == nil && rd.APIKey != "" {
		rc._gi4p = rd.APIKey
		fmt.Printf("  ✓ License found: %s...%s\n", rd.APIKey[:8], rd.APIKey[len(rd.APIKey)-4:])

		rc._wx = sha256.Sum256([]byte(rc._gi4p + rc._mf41))
		rc._kjn.Store(true)
		ActivateIntegrity(rc)
		fmt.Println("  ✓ License activated successfully")

		go func() {
			if err := _ty(rc, _wy); err != nil {
				fmt.Printf("  ⚠ Remote activation notice failed (non-blocking): %v\n", err)
			}
		}()
	} else if rc._lg0 != "" {
		rc._gi4p = rc._lg0
		if err := _ty(rc, _wy); err == nil {
			_cyg(&RuntimeData{APIKey: rc._lg0, Tier: _che})
			rc._wx = sha256.Sum256([]byte(rc._gi4p + rc._mf41))
			rc._kjn.Store(true)
			ActivateIntegrity(rc)
			fmt.Printf("  ✓ GLOBAL_API_KEY accepted — license saved and activated\n")
		} else {
			rc._gi4p = ""
			_fa4()
			rc._kjn.Store(false)
		}
	} else {
		_fa4()
		rc._kjn.Store(false)
	}

	return rc
}

func _fa4() {
	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════════════════════╗")
	fmt.Println("  ║              License Registration Required               ║")
	fmt.Println("  ╚══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("  Server starting without license.")
	fmt.Println("  API endpoints will return 503 until license is activated.")
	fmt.Println("  Use GET /license/register to get the registration URL.")
	fmt.Println()
}

func (rc *RuntimeContext) _6n(authCodeOrKey, _che string, customerID int) error {
	_gi4p, err := _16i(authCodeOrKey)
	if err != nil {
		return fmt.Errorf("key exchange failed: %w", err)
	}

	rc.mu.Lock()
	rc._gi4p = _gi4p
	rc._83j = ""
	rc._whv = ""
	rc.mu.Unlock()

	if err := _cyg(&RuntimeData{
		APIKey:     _gi4p,
		Tier:       _che,
		CustomerID: customerID,
	}); err != nil {
		fmt.Printf("  ⚠ Warning: could not save license: %v\n", err)
	}

	if err := _ty(rc, rc._wy); err != nil {
		return err
	}

	rc.mu.Lock()
	rc._wx = sha256.Sum256([]byte(rc._gi4p + rc._mf41))
	rc.mu.Unlock()
	rc._kjn.Store(true)
	ActivateIntegrity(rc)

	fmt.Printf("  ✓ License activated! Key: %s...%s (_che: %s)\n",
		_gi4p[:8], _gi4p[len(_gi4p)-4:], _che)

	go func() {
		if err := _ylf(rc, 0); err != nil {
			fmt.Printf("  ⚠ First heartbeat failed: %v\n", err)
		}
	}()

	return nil
}

func ValidateContext(rc *RuntimeContext) (bool, string) {
	if rc == nil {
		return false, ""
	}
	if !rc._kjn.Load() {
		return false, rc.RegistrationURL()
	}
	expected := sha256.Sum256([]byte(rc._gi4p + rc._mf41))
	actual := rc.ContextHash()
	if expected != actual {
		return false, ""
	}
	return true, ""
}

func GateMiddleware(rc *RuntimeContext) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		if path == "/health" || path == "/server/ok" || path == "/favicon.ico" ||
			path == "/license/status" || path == "/license/register" || path == "/license/activate" ||
			strings.HasPrefix(path, "/manager") || strings.HasPrefix(path, "/assets") ||
			strings.HasPrefix(path, "/swagger") || path == "/ws" ||
			strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".css") ||
			strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".png") ||
			strings.HasSuffix(path, ".ico") || strings.HasSuffix(path, ".woff2") ||
			strings.HasSuffix(path, ".woff") || strings.HasSuffix(path, ".ttf") {
			c.Next()
			return
		}

		valid, _ := ValidateContext(rc)
		if !valid {
			scheme := "http"
			if c.Request.TLS != nil {
				scheme = "https"
			}
			managerURL := fmt.Sprintf("%s://%s/manager/login", scheme, c.Request.Host)

			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":        "service not activated",
				"code":         "LICENSE_REQUIRED",
				"register_url": managerURL,
				"message":      "License required. Open the manager to activate your license.",
			})
			return
		}

		c.Set("_rch", rc.ContextHash())
		c.Next()
	}
}

func LicenseRoutes(eng *gin.Engine, rc *RuntimeContext) {
	lic := eng.Group("/license")
	{
		lic.GET("/status", func(c *gin.Context) {
			status := "inactive"
			if rc.IsActive() {
				status = "active"
			}

			resp := gin.H{
				"status":      status,
				"instance_id": rc._mf41,
			}

			rc.mu.RLock()
			if rc._gi4p != "" {
				resp["api_key"] = rc._gi4p[:8] + "..." + rc._gi4p[len(rc._gi4p)-4:]
			}
			rc.mu.RUnlock()

			c.JSON(http.StatusOK, resp)
		})

		lic.GET("/register", func(c *gin.Context) {
			if rc.IsActive() {
				c.JSON(http.StatusOK, gin.H{
					"status":  "active",
					"message": "License is already active",
				})
				return
			}

			rc.mu.RLock()
			existingURL := rc._83j
			rc.mu.RUnlock()

			if existingURL != "" {
				c.JSON(http.StatusOK, gin.H{
					"status":       "pending",
					"register_url": existingURL,
				})
				return
			}

			payload := map[string]string{
				"tier":        rc._che,
				"version":     rc._wy,
				"instance_id": rc._mf41,
			}
			if redirectURI := c.Query("redirect_uri"); redirectURI != "" {
				payload["redirect_uri"] = redirectURI
			}

			resp, err := _ef1m("/v1/register/init", payload)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{
					"error":   "Failed to contact licensing server",
					"details": err.Error(),
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				_rsbr := _ch(resp)
				c.JSON(resp.StatusCode, gin.H{
					"error":   "Licensing server error",
					"details": _rsbr.Error(),
				})
				return
			}

			var _eb struct {
				RegisterURL string `json:"register_url"`
				Token       string `json:"token"`
			}
			json.NewDecoder(resp.Body).Decode(&_eb)

			rc.mu.Lock()
			rc._83j = _eb.RegisterURL
			rc._whv = _eb.Token
			rc.mu.Unlock()

			fmt.Printf("  → Registration URL: %s\n", _eb.RegisterURL)

			c.JSON(http.StatusOK, gin.H{
				"status":       "pending",
				"register_url": _eb.RegisterURL,
			})
		})

		lic.GET("/activate", func(c *gin.Context) {
			if rc.IsActive() {
				c.JSON(http.StatusOK, gin.H{
					"status":  "active",
					"message": "License is already active",
				})
				return
			}

			code := c.Query("code")
			if code == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "Missing code parameter",
					"message": "Provide ?code=AUTHORIZATION_CODE from the registration callback.",
				})
				return
			}

			exchangeResp, err := _ef1m("/v1/register/exchange", map[string]string{
				"authorization_code": code,
				"instance_id":       rc._mf41,
			})
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{
					"error":   "Failed to contact licensing server",
					"details": err.Error(),
				})
				return
			}
			defer exchangeResp.Body.Close()

			if exchangeResp.StatusCode != http.StatusOK {
				_rsbr := _ch(exchangeResp)
				c.JSON(exchangeResp.StatusCode, gin.H{
					"error":   "Exchange failed",
					"details": _rsbr.Error(),
				})
				return
			}

			var _2p struct {
				APIKey     string `json:"api_key"`
				Tier       string `json:"tier"`
				CustomerID int    `json:"customer_id"`
			}
			json.NewDecoder(exchangeResp.Body).Decode(&_2p)

			if _2p.APIKey == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "Invalid or expired code",
					"message": "The authorization code is invalid or has expired.",
				})
				return
			}

			if err := rc._6n(_2p.APIKey, _2p.Tier, _2p.CustomerID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "Activation failed",
					"details": err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status":  "active",
				"message": "License activated successfully!",
			})
		})
	}
}

func StartHeartbeat(ctx context.Context, rc *RuntimeContext, startTime time.Time) {
	go func() {
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !rc.IsActive() {
					continue
				}
				uptime := int64(time.Since(startTime).Seconds())
				if err := _ylf(rc, uptime); err != nil {
					fmt.Printf("  ⚠ Heartbeat failed (non-blocking): %v\n", err)
				}
			}
		}
	}()
}

func Shutdown(rc *RuntimeContext) {
	if rc == nil || rc._gi4p == "" {
		return
	}
	_7m53(rc)
}

func _njn(code string) (_gi4p string, err error) {
	resp, err := _ef1m("/v1/register/exchange", map[string]string{
		"authorization_code": code,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", _ch(resp)
	}

	var _2p struct {
		APIKey string `json:"api_key"`
	}
	json.NewDecoder(resp.Body).Decode(&_2p)
	if _2p.APIKey == "" {
		return "", fmt.Errorf("exchange returned empty api_key")
	}
	return _2p.APIKey, nil
}

func _16i(authCodeOrKey string) (string, error) {
	_gi4p, err := _njn(authCodeOrKey)
	if err == nil && _gi4p != "" {
		return _gi4p, nil
	}
	return authCodeOrKey, nil
}

func _ty(rc *RuntimeContext, _wy string) error {
	resp, err := _1jm("/v1/activate", map[string]string{
		"instance_id": rc._mf41,
		"version":     _wy,
	}, rc._gi4p)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _ch(resp)
	}

	var _2p struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&_2p)

	if _2p.Status != "active" {
		return fmt.Errorf("activation returned status: %s", _2p.Status)
	}
	return nil
}

func _ylf(rc *RuntimeContext, uptimeSeconds int64) error {
	resp, err := _1jm("/v1/heartbeat", map[string]any{
		"instance_id":    rc._mf41,
		"uptime_seconds": uptimeSeconds,
		"version":        rc._wy,
	}, rc._gi4p)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _ch(resp)
	}
	return nil
}

func _7m53(rc *RuntimeContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{
		"instance_id": rc._mf41,
	})

	url := _qo() + "/v1/deactivate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", rc._gi4p)
	req.Header.Set("X-Signature", _41(body, rc._gi4p))
	_dewb.Do(req)
}

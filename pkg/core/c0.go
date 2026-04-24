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

var _k1 = []byte{0xc4, 0x8c, 0x65, 0x38, 0x2b, 0x09, 0xb6, 0xe6, 0x3b, 0x8b, 0x75, 0x3f, 0xde, 0x35, 0x3a, 0x9e, 0xac, 0xda, 0xb6, 0x24, 0x3d, 0x02, 0xa5, 0x3f, 0x00, 0x7c, 0xb8, 0x65, 0x6a, 0x76, 0x21, 0xb8, 0x3e, 0x33, 0xe3, 0x4d, 0x69, 0xce, 0x9b, 0x13, 0x18, 0x9a}
var _k0 = []byte{0xac, 0xf8, 0x11, 0x48, 0x58, 0x33, 0x99, 0xc9, 0x57, 0xe2, 0x16, 0x5a, 0xb0, 0x46, 0x5f, 0xb0, 0xc9, 0xac, 0xd9, 0x48, 0x48, 0x76, 0xcc, 0x50, 0x6e, 0x1a, 0xd7, 0x10, 0x04, 0x12, 0x40, 0xcc, 0x57, 0x5c, 0x8d, 0x63, 0x0a, 0xa1, 0xf6, 0x3d, 0x7a, 0xe8}

var (
	_ub1 string
	_z8    string
)

func _rn() string {
	if _ub1 != "" && _z8 != "" {
		return _ty65(_ub1, _z8)
	}
	parts := [...]string{"h", "tt", "ps", "://", "li", "ce", "nse", ".", "ev", "ol", "ut", "io", "nf", "ou", "nd", "at", "io", "n.", "co", "m.", "br"}
	var s string
	for _, p := range parts {
		s += p
	}
	return s
}

func _ty65(enc, key string) string {
	encBytes := _i7qj(enc)
	keyBytes := _i7qj(key)
	if len(keyBytes) == 0 {
		return ""
	}
	out := make([]byte, len(encBytes))
	for i, b := range encBytes {
		out[i] = b ^ keyBytes[i%len(keyBytes)]
	}
	return string(out)
}

func _i7qj(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b[i/2] = _zpk(s[i])<<4 | _zpk(s[i+1])
	}
	return b
}

func _zpk(c byte) byte {
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

var _e53m = &http.Client{Timeout: 10 * time.Second}

func _xh24(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func _28j0(path string, payload interface{}, _97 string) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _rn() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", _97)
	req.Header.Set("X-Signature", _xh24(body, _97))

	return _e53m.Do(req)
}

func _38(path string) (*http.Response, error) {
	url := _rn() + path
	return _e53m.Get(url)
}

func _h0z3(path string, payload interface{}) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _rn() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return _e53m.Do(req)
}

func _jy3(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	var _m2q struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(b, &_m2q); err == nil {
		msg := _m2q.Message
		if msg == "" {
			msg = _m2q.Error
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

var _io *gorm.DB

func SetDB(db *gorm.DB) {
	_io = db
}

func MigrateDB() error {
	if _io == nil {
		return fmt.Errorf("core: database not set, call SetDB first")
	}
	return _io.AutoMigrate(&RuntimeConfig{})
}

func _dnog(key string) (string, error) {
	if _io == nil {
		return "", fmt.Errorf("core: database not set")
	}
	var _nes8 RuntimeConfig
	_4qch := _io.Where("key = ?", key).First(&_nes8)
	if _4qch.Error != nil {
		return "", _4qch.Error
	}
	return _nes8.Value, nil
}

func _wg7(key, value string) error {
	if _io == nil {
		return fmt.Errorf("core: database not set")
	}
	var _nes8 RuntimeConfig
	_4qch := _io.Where("key = ?", key).First(&_nes8)
	if _4qch.Error != nil {
		return _io.Create(&RuntimeConfig{Key: key, Value: value}).Error
	}
	return _io.Model(&_nes8).Update("value", value).Error
}

func _apup(key string) {
	if _io == nil {
		return
	}
	_io.Where("key = ?", key).Delete(&RuntimeConfig{})
}

type RuntimeData struct {
	APIKey     string
	Tier       string
	CustomerID int
}

func _wl() (*RuntimeData, error) {
	_97, err := _dnog(ConfigKeyAPIKey)
	if err != nil || _97 == "" {
		return nil, fmt.Errorf("no license found")
	}

	_y8, _ := _dnog(ConfigKeyTier)
	customerIDStr, _ := _dnog(ConfigKeyCustomerID)
	customerID, _ := strconv.Atoi(customerIDStr)

	return &RuntimeData{
		APIKey:     _97,
		Tier:       _y8,
		CustomerID: customerID,
	}, nil
}

func _sy(rd *RuntimeData) error {
	if err := _wg7(ConfigKeyAPIKey, rd.APIKey); err != nil {
		return err
	}
	if err := _wg7(ConfigKeyTier, rd.Tier); err != nil {
		return err
	}
	if rd.CustomerID > 0 {
		if err := _wg7(ConfigKeyCustomerID, strconv.Itoa(rd.CustomerID)); err != nil {
			return err
		}
	}
	return nil
}

func _vw() {
	_apup(ConfigKeyAPIKey)
	_apup(ConfigKeyTier)
	_apup(ConfigKeyCustomerID)
}

func _pbl5() (string, error) {
	id, err := _dnog(ConfigKeyInstanceID)
	if err == nil && len(id) == 36 {
		return id, nil
	}

	id = _q4lc()
	if id == "" {
		id, err = _w5ch()
		if err != nil {
			return "", err
		}
	}

	if err := _wg7(ConfigKeyInstanceID, id); err != nil {
		return "", err
	}
	return id, nil
}

func _q4lc() string {
	hostname, _ := os.Hostname()
	macAddr := _u40()
	if hostname == "" && macAddr == "" {
		return ""
	}

	seed := hostname + "|" + macAddr
	h := make([]byte, 16)
	copy(h, []byte(seed))
	for i := 16; i < len(seed); i++ {
		h[i%16] ^= seed[i]
	}
	h[6] = (h[6] & 0x0f) | 0x40 // _on5 4
	h[8] = (h[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func _u40() string {
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

func _w5ch() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

var _hwq5 atomic.Value // set during activation

func init() {
	_hwq5.Store([]byte{0})
}

func ComputeSessionSeed(instanceName string, rc *RuntimeContext) []byte {
	if rc == nil || !rc._hec.Load() {
		return nil // Will cause panic in caller — intentional
	}
	h := sha256.New()
	h.Write([]byte(instanceName))
	h.Write([]byte(rc._97))
	salt, _ := _hwq5.Load().([]byte)
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

func DeriveInstanceToken(_0rb string, rc *RuntimeContext) string {
	if rc == nil || !rc._hec.Load() {
		return ""
	}
	h := sha256.Sum256([]byte(_0rb + rc._97))
	return _ivd(h[:8])
}

func _ivd(b []byte) string {
	const _i1ad = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = _i1ad[v>>4]
		dst[i*2+1] = _i1ad[v&0x0f]
	}
	return string(dst)
}

func ActivateIntegrity(rc *RuntimeContext) {
	if rc == nil {
		return
	}
	h := sha256.Sum256([]byte(rc._97 + rc._0rb + "ev0"))
	_hwq5.Store(h[:])
}

const (
	hbInterval = 30 * time.Minute
)

type RuntimeContext struct {
	_97       string
	_tg string // GLOBAL_API_KEY from .env — used as token for licensing check
	_0rb   string
	_hec       atomic.Bool
	_tu      [32]byte // Derived from activation — required by ValidateContext
	mu           sync.RWMutex
	_pc       string // Registration URL shown to users before activation
	_hls     string // Registration token for polling
	_y8         string
	_on5      string
	_y3      atomic.Int64 // Messages sent since last heartbeat
	_fbb0      atomic.Int64 // Messages received since last heartbeat
}

var _689z atomic.Pointer[RuntimeContext]

func (rc *RuntimeContext) TrackMessage() {
	if rc != nil {
		rc._y3.Add(1)
	}
}

func TrackMessageSent() {
	if rc := _689z.Load(); rc != nil {
		rc._y3.Add(1)
	}
}

func TrackMessageRecv() {
	if rc := _689z.Load(); rc != nil {
		rc._fbb0.Add(1)
	}
}

func (rc *RuntimeContext) _he() int64 {
	return rc._y3.Swap(0)
}

func (rc *RuntimeContext) ContextHash() [32]byte {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._tu
}

func (rc *RuntimeContext) IsActive() bool {
	return rc._hec.Load()
}

func (rc *RuntimeContext) RegistrationURL() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._pc
}

func (rc *RuntimeContext) APIKey() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._97
}

func (rc *RuntimeContext) InstanceID() string {
	return rc._0rb
}

func InitializeRuntime(_y8, _on5, _tg string) *RuntimeContext {
	if _y8 == "" {
		_y8 = "evolution-go"
	}
	if _on5 == "" {
		_on5 = "unknown"
	}

	rc := &RuntimeContext{
		_y8:         _y8,
		_on5:      _on5,
		_tg: _tg,
	}

	id, err := _pbl5()
	if err != nil {
		log.Fatalf("[runtime] failed to initialize instance: %v", err)
	}
	rc._0rb = id

	rd, err := _wl()
	if err == nil && rd.APIKey != "" {
		rc._97 = rd.APIKey
		fmt.Printf("  ✓ License found: %s...%s\n", rd.APIKey[:8], rd.APIKey[len(rd.APIKey)-4:])

		rc._tu = sha256.Sum256([]byte(rc._97 + rc._0rb))
		rc._hec.Store(true)
		ActivateIntegrity(rc)
		fmt.Println("  ✓ License activated successfully")

		go func() {
			if err := _twt7(rc, _on5); err != nil {
				fmt.Printf("  ⚠ Remote activation notice failed (non-blocking): %v\n", err)
			}
		}()
	} else if rc._tg != "" {
		rc._97 = rc._tg
		if err := _twt7(rc, _on5); err == nil {
			_sy(&RuntimeData{APIKey: rc._tg, Tier: _y8})
			rc._tu = sha256.Sum256([]byte(rc._97 + rc._0rb))
			rc._hec.Store(true)
			ActivateIntegrity(rc)
			fmt.Printf("  ✓ GLOBAL_API_KEY accepted — license saved and activated\n")
		} else {
			rc._97 = ""
			_27()
			rc._hec.Store(false)
		}
	} else {
		_27()
		rc._hec.Store(false)
	}

	_689z.Store(rc)

	return rc
}

func _27() {
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

func (rc *RuntimeContext) _60(authCodeOrKey, _y8 string, customerID int) error {
	_97, err := _aqt2(authCodeOrKey)
	if err != nil {
		return fmt.Errorf("key exchange failed: %w", err)
	}

	rc.mu.Lock()
	rc._97 = _97
	rc._pc = ""
	rc._hls = ""
	rc.mu.Unlock()

	if err := _sy(&RuntimeData{
		APIKey:     _97,
		Tier:       _y8,
		CustomerID: customerID,
	}); err != nil {
		fmt.Printf("  ⚠ Warning: could not save license: %v\n", err)
	}

	if err := _twt7(rc, rc._on5); err != nil {
		return err
	}

	rc.mu.Lock()
	rc._tu = sha256.Sum256([]byte(rc._97 + rc._0rb))
	rc.mu.Unlock()
	rc._hec.Store(true)
	ActivateIntegrity(rc)

	fmt.Printf("  ✓ License activated! Key: %s...%s (_y8: %s)\n",
		_97[:8], _97[len(_97)-4:], _y8)

	go func() {
		if err := _e0w7(rc, 0); err != nil {
			fmt.Printf("  ⚠ First heartbeat failed: %v\n", err)
		}
	}()

	return nil
}

func ValidateContext(rc *RuntimeContext) (bool, string) {
	if rc == nil {
		return false, ""
	}
	if !rc._hec.Load() {
		return false, rc.RegistrationURL()
	}
	expected := sha256.Sum256([]byte(rc._97 + rc._0rb))
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
				"instance_id": rc._0rb,
			}

			rc.mu.RLock()
			if rc._97 != "" {
				resp["api_key"] = rc._97[:8] + "..." + rc._97[len(rc._97)-4:]
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
			existingURL := rc._pc
			rc.mu.RUnlock()

			if existingURL != "" {
				c.JSON(http.StatusOK, gin.H{
					"status":       "pending",
					"register_url": existingURL,
				})
				return
			}

			payload := map[string]string{
				"tier":        rc._y8,
				"version":     rc._on5,
				"instance_id": rc._0rb,
			}
			if redirectURI := c.Query("redirect_uri"); redirectURI != "" {
				payload["redirect_uri"] = redirectURI
			}

			resp, err := _h0z3("/v1/register/init", payload)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{
					"error":   "Failed to contact licensing server",
					"details": err.Error(),
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				_m2q := _jy3(resp)
				c.JSON(resp.StatusCode, gin.H{
					"error":   "Licensing server error",
					"details": _m2q.Error(),
				})
				return
			}

			var _sy1l struct {
				RegisterURL string `json:"register_url"`
				Token       string `json:"token"`
			}
			json.NewDecoder(resp.Body).Decode(&_sy1l)

			rc.mu.Lock()
			rc._pc = _sy1l.RegisterURL
			rc._hls = _sy1l.Token
			rc.mu.Unlock()

			fmt.Printf("  → Registration URL: %s\n", _sy1l.RegisterURL)

			c.JSON(http.StatusOK, gin.H{
				"status":       "pending",
				"register_url": _sy1l.RegisterURL,
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

			exchangeResp, err := _h0z3("/v1/register/exchange", map[string]string{
				"authorization_code": code,
				"instance_id":       rc._0rb,
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
				_m2q := _jy3(exchangeResp)
				c.JSON(exchangeResp.StatusCode, gin.H{
					"error":   "Exchange failed",
					"details": _m2q.Error(),
				})
				return
			}

			var _4qch struct {
				APIKey     string `json:"api_key"`
				Tier       string `json:"tier"`
				CustomerID int    `json:"customer_id"`
			}
			json.NewDecoder(exchangeResp.Body).Decode(&_4qch)

			if _4qch.APIKey == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "Invalid or expired code",
					"message": "The authorization code is invalid or has expired.",
				})
				return
			}

			if err := rc._60(_4qch.APIKey, _4qch.Tier, _4qch.CustomerID); err != nil {
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
				if err := _e0w7(rc, uptime); err != nil {
					fmt.Printf("  ⚠ Heartbeat failed (non-blocking): %v\n", err)
				}
			}
		}
	}()
}

func Shutdown(rc *RuntimeContext) {
	if rc == nil || rc._97 == "" {
		return
	}
	_vs9d(rc)
}

func _o1(code string) (_97 string, err error) {
	resp, err := _h0z3("/v1/register/exchange", map[string]string{
		"authorization_code": code,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", _jy3(resp)
	}

	var _4qch struct {
		APIKey string `json:"api_key"`
	}
	json.NewDecoder(resp.Body).Decode(&_4qch)
	if _4qch.APIKey == "" {
		return "", fmt.Errorf("exchange returned empty api_key")
	}
	return _4qch.APIKey, nil
}

func _aqt2(authCodeOrKey string) (string, error) {
	_97, err := _o1(authCodeOrKey)
	if err == nil && _97 != "" {
		return _97, nil
	}
	return authCodeOrKey, nil
}

func _twt7(rc *RuntimeContext, _on5 string) error {
	resp, err := _28j0("/v1/activate", map[string]string{
		"instance_id": rc._0rb,
		"version":     _on5,
	}, rc._97)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _jy3(resp)
	}

	var _4qch struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&_4qch)

	if _4qch.Status != "active" {
		return fmt.Errorf("activation returned status: %s", _4qch.Status)
	}
	return nil
}

func _e0w7(rc *RuntimeContext, uptimeSeconds int64) error {
	_y3 := rc._he()
	_fbb0 := rc._fbb0.Swap(0)

	payload := map[string]any{
		"instance_id":    rc._0rb,
		"uptime_seconds": uptimeSeconds,
		"version":        rc._on5,
	}

	if _y3 > 0 || _fbb0 > 0 {
		bundle := map[string]any{}
		if _y3 > 0 {
			bundle["messages_sent"] = _y3
		}
		if _fbb0 > 0 {
			bundle["messages_recv"] = _fbb0
		}
		payload["telemetry_bundle"] = bundle
	}

	resp, err := _28j0("/v1/heartbeat", payload, rc._97)
	if err != nil {
		rc._y3.Add(_y3)
		rc._fbb0.Add(_fbb0)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rc._y3.Add(_y3)
		rc._fbb0.Add(_fbb0)
		return _jy3(resp)
	}
	return nil
}

func _vs9d(rc *RuntimeContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{
		"instance_id": rc._0rb,
	})

	url := _rn() + "/v1/deactivate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", rc._97)
	req.Header.Set("X-Signature", _xh24(body, rc._97))
	_e53m.Do(req)
}

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

var _k1 = []byte{0x89, 0xdb, 0x39, 0x89, 0x5e, 0x48, 0x4c, 0xc7, 0xbb, 0x2f, 0x65, 0xf7, 0x88, 0x97, 0xc5, 0x92, 0x7d, 0x36, 0x67, 0xc9, 0x38, 0xcd, 0x79, 0xe7, 0x90, 0x2f, 0xd6, 0x91, 0x9d, 0xdb, 0xd1, 0x3e, 0xca, 0x19, 0x63, 0xdf, 0x22, 0x92, 0x2e, 0x6d, 0x8a, 0x04}
var _k0 = []byte{0xe1, 0xaf, 0x4d, 0xf9, 0x2d, 0x72, 0x63, 0xe8, 0xd7, 0x46, 0x06, 0x92, 0xe6, 0xe4, 0xa0, 0xbc, 0x18, 0x40, 0x08, 0xa5, 0x4d, 0xb9, 0x10, 0x88, 0xfe, 0x49, 0xb9, 0xe4, 0xf3, 0xbf, 0xb0, 0x4a, 0xa3, 0x76, 0x0d, 0xf1, 0x41, 0xfd, 0x43, 0x43, 0xe8, 0x76}

var (
	_w96 string
	_kuc4    string
)

func _k5qk() string {
	if _w96 != "" && _kuc4 != "" {
		return _wk(_w96, _kuc4)
	}
	parts := [...]string{"h", "tt", "ps", "://", "li", "ce", "nse", ".", "ev", "ol", "ut", "io", "nf", "ou", "nd", "at", "io", "n.", "co", "m.", "br"}
	var s string
	for _, p := range parts {
		s += p
	}
	return s
}

func _wk(enc, key string) string {
	encBytes := _8m8(enc)
	keyBytes := _8m8(key)
	if len(keyBytes) == 0 {
		return ""
	}
	out := make([]byte, len(encBytes))
	for i, b := range encBytes {
		out[i] = b ^ keyBytes[i%len(keyBytes)]
	}
	return string(out)
}

func _8m8(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b[i/2] = _0u1(s[i])<<4 | _0u1(s[i+1])
	}
	return b
}

func _0u1(c byte) byte {
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

var _dw = &http.Client{Timeout: 10 * time.Second}

func _hw1c(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func _ys(path string, payload interface{}, _4g string) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _k5qk() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", _4g)
	req.Header.Set("X-Signature", _hw1c(body, _4g))

	return _dw.Do(req)
}

func _0k9k(path string) (*http.Response, error) {
	url := _k5qk() + path
	return _dw.Get(url)
}

func _j4(path string, payload interface{}) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _k5qk() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return _dw.Do(req)
}

func _fku(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	var _iweb struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(b, &_iweb); err == nil {
		msg := _iweb.Message
		if msg == "" {
			msg = _iweb.Error
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

var _qsv *gorm.DB

func SetDB(db *gorm.DB) {
	_qsv = db
}

func MigrateDB() error {
	if _qsv == nil {
		return fmt.Errorf("core: database not set, call SetDB first")
	}
	return _qsv.AutoMigrate(&RuntimeConfig{})
}

func _bmg(key string) (string, error) {
	if _qsv == nil {
		return "", fmt.Errorf("core: database not set")
	}
	var _lejk RuntimeConfig
	_hf := _qsv.Where("key = ?", key).First(&_lejk)
	if _hf.Error != nil {
		return "", _hf.Error
	}
	return _lejk.Value, nil
}

func _sub(key, value string) error {
	if _qsv == nil {
		return fmt.Errorf("core: database not set")
	}
	var _lejk RuntimeConfig
	_hf := _qsv.Where("key = ?", key).First(&_lejk)
	if _hf.Error != nil {
		return _qsv.Create(&RuntimeConfig{Key: key, Value: value}).Error
	}
	return _qsv.Model(&_lejk).Update("value", value).Error
}

func _yq(key string) {
	if _qsv == nil {
		return
	}
	_qsv.Where("key = ?", key).Delete(&RuntimeConfig{})
}

type RuntimeData struct {
	APIKey     string
	Tier       string
	CustomerID int
}

func _vl() (*RuntimeData, error) {
	_4g, err := _bmg(ConfigKeyAPIKey)
	if err != nil || _4g == "" {
		return nil, fmt.Errorf("no license found")
	}

	_0jkp, _ := _bmg(ConfigKeyTier)
	customerIDStr, _ := _bmg(ConfigKeyCustomerID)
	customerID, _ := strconv.Atoi(customerIDStr)

	return &RuntimeData{
		APIKey:     _4g,
		Tier:       _0jkp,
		CustomerID: customerID,
	}, nil
}

func _esnc(rd *RuntimeData) error {
	if err := _sub(ConfigKeyAPIKey, rd.APIKey); err != nil {
		return err
	}
	if err := _sub(ConfigKeyTier, rd.Tier); err != nil {
		return err
	}
	if rd.CustomerID > 0 {
		if err := _sub(ConfigKeyCustomerID, strconv.Itoa(rd.CustomerID)); err != nil {
			return err
		}
	}
	return nil
}

func _lgxq() {
	_yq(ConfigKeyAPIKey)
	_yq(ConfigKeyTier)
	_yq(ConfigKeyCustomerID)
}

func _6z() (string, error) {
	id, err := _bmg(ConfigKeyInstanceID)
	if err == nil && len(id) == 36 {
		return id, nil
	}

	id = _g2t()
	if id == "" {
		id, err = _7cb()
		if err != nil {
			return "", err
		}
	}

	if err := _sub(ConfigKeyInstanceID, id); err != nil {
		return "", err
	}
	return id, nil
}

func _g2t() string {
	hostname, _ := os.Hostname()
	macAddr := _ayb1()
	if hostname == "" && macAddr == "" {
		return ""
	}

	seed := hostname + "|" + macAddr
	h := make([]byte, 16)
	copy(h, []byte(seed))
	for i := 16; i < len(seed); i++ {
		h[i%16] ^= seed[i]
	}
	h[6] = (h[6] & 0x0f) | 0x40 // _r3j 4
	h[8] = (h[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func _ayb1() string {
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

func _7cb() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

var _2e atomic.Value // set during activation

func init() {
	_2e.Store([]byte{0})
}

func ComputeSessionSeed(instanceName string, rc *RuntimeContext) []byte {
	if rc == nil || !rc._72.Load() {
		return nil // Will cause panic in caller — intentional
	}
	h := sha256.New()
	h.Write([]byte(instanceName))
	h.Write([]byte(rc._4g))
	salt, _ := _2e.Load().([]byte)
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

func DeriveInstanceToken(_sk string, rc *RuntimeContext) string {
	if rc == nil || !rc._72.Load() {
		return ""
	}
	h := sha256.Sum256([]byte(_sk + rc._4g))
	return _vd(h[:8])
}

func _vd(b []byte) string {
	const _msj = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = _msj[v>>4]
		dst[i*2+1] = _msj[v&0x0f]
	}
	return string(dst)
}

func ActivateIntegrity(rc *RuntimeContext) {
	if rc == nil {
		return
	}
	h := sha256.Sum256([]byte(rc._4g + rc._sk + "ev0"))
	_2e.Store(h[:])
}

const (
	hbInterval = 30 * time.Minute
)

type RuntimeContext struct {
	_4g       string
	_brf string // GLOBAL_API_KEY from .env — used as token for licensing check
	_sk   string
	_72       atomic.Bool
	_zcn      [32]byte // Derived from activation — required by ValidateContext
	mu           sync.RWMutex
	_191       string // Registration URL shown to users before activation
	_ou     string // Registration token for polling
	_0jkp         string
	_r3j      string
}

func (rc *RuntimeContext) ContextHash() [32]byte {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._zcn
}

func (rc *RuntimeContext) IsActive() bool {
	return rc._72.Load()
}

func (rc *RuntimeContext) RegistrationURL() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._191
}

func (rc *RuntimeContext) APIKey() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._4g
}

func (rc *RuntimeContext) InstanceID() string {
	return rc._sk
}

func InitializeRuntime(_0jkp, _r3j, _brf string) *RuntimeContext {
	if _0jkp == "" {
		_0jkp = "evolution-go"
	}
	if _r3j == "" {
		_r3j = "unknown"
	}

	rc := &RuntimeContext{
		_0jkp:         _0jkp,
		_r3j:      _r3j,
		_brf: _brf,
	}

	id, err := _6z()
	if err != nil {
		log.Fatalf("[runtime] failed to initialize instance: %v", err)
	}
	rc._sk = id

	rd, err := _vl()
	if err == nil && rd.APIKey != "" {
		rc._4g = rd.APIKey
		fmt.Printf("  ✓ License found: %s...%s\n", rd.APIKey[:8], rd.APIKey[len(rd.APIKey)-4:])

		rc._zcn = sha256.Sum256([]byte(rc._4g + rc._sk))
		rc._72.Store(true)
		ActivateIntegrity(rc)
		fmt.Println("  ✓ License activated successfully")

		go func() {
			if err := _wlr(rc, _r3j); err != nil {
				fmt.Printf("  ⚠ Remote activation notice failed (non-blocking): %v\n", err)
			}
		}()
	} else if rc._brf != "" {
		rc._4g = rc._brf
		if err := _wlr(rc, _r3j); err == nil {
			_esnc(&RuntimeData{APIKey: rc._brf, Tier: _0jkp})
			rc._zcn = sha256.Sum256([]byte(rc._4g + rc._sk))
			rc._72.Store(true)
			ActivateIntegrity(rc)
			fmt.Printf("  ✓ GLOBAL_API_KEY accepted — license saved and activated\n")
		} else {
			rc._4g = ""
			_06e()
			rc._72.Store(false)
		}
	} else {
		_06e()
		rc._72.Store(false)
	}

	return rc
}

func _06e() {
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

func (rc *RuntimeContext) _mz06(authCodeOrKey, _0jkp string, customerID int) error {
	_4g, err := _u7(authCodeOrKey)
	if err != nil {
		return fmt.Errorf("key exchange failed: %w", err)
	}

	rc.mu.Lock()
	rc._4g = _4g
	rc._191 = ""
	rc._ou = ""
	rc.mu.Unlock()

	if err := _esnc(&RuntimeData{
		APIKey:     _4g,
		Tier:       _0jkp,
		CustomerID: customerID,
	}); err != nil {
		fmt.Printf("  ⚠ Warning: could not save license: %v\n", err)
	}

	if err := _wlr(rc, rc._r3j); err != nil {
		return err
	}

	rc.mu.Lock()
	rc._zcn = sha256.Sum256([]byte(rc._4g + rc._sk))
	rc.mu.Unlock()
	rc._72.Store(true)
	ActivateIntegrity(rc)

	fmt.Printf("  ✓ License activated! Key: %s...%s (_0jkp: %s)\n",
		_4g[:8], _4g[len(_4g)-4:], _0jkp)

	go func() {
		if err := _mbz(rc, 0); err != nil {
			fmt.Printf("  ⚠ First heartbeat failed: %v\n", err)
		}
	}()

	return nil
}

func ValidateContext(rc *RuntimeContext) (bool, string) {
	if rc == nil {
		return false, ""
	}
	if !rc._72.Load() {
		return false, rc.RegistrationURL()
	}
	expected := sha256.Sum256([]byte(rc._4g + rc._sk))
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
				"instance_id": rc._sk,
			}

			rc.mu.RLock()
			if rc._4g != "" {
				resp["api_key"] = rc._4g[:8] + "..." + rc._4g[len(rc._4g)-4:]
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
			existingURL := rc._191
			rc.mu.RUnlock()

			if existingURL != "" {
				c.JSON(http.StatusOK, gin.H{
					"status":       "pending",
					"register_url": existingURL,
				})
				return
			}

			payload := map[string]string{
				"tier":        rc._0jkp,
				"version":     rc._r3j,
				"instance_id": rc._sk,
			}
			if redirectURI := c.Query("redirect_uri"); redirectURI != "" {
				payload["redirect_uri"] = redirectURI
			}

			resp, err := _j4("/v1/register/init", payload)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{
					"error":   "Failed to contact licensing server",
					"details": err.Error(),
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				_iweb := _fku(resp)
				c.JSON(resp.StatusCode, gin.H{
					"error":   "Licensing server error",
					"details": _iweb.Error(),
				})
				return
			}

			var _75 struct {
				RegisterURL string `json:"register_url"`
				Token       string `json:"token"`
			}
			json.NewDecoder(resp.Body).Decode(&_75)

			rc.mu.Lock()
			rc._191 = _75.RegisterURL
			rc._ou = _75.Token
			rc.mu.Unlock()

			fmt.Printf("  → Registration URL: %s\n", _75.RegisterURL)

			c.JSON(http.StatusOK, gin.H{
				"status":       "pending",
				"register_url": _75.RegisterURL,
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

			exchangeResp, err := _j4("/v1/register/exchange", map[string]string{
				"authorization_code": code,
				"instance_id":       rc._sk,
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
				_iweb := _fku(exchangeResp)
				c.JSON(exchangeResp.StatusCode, gin.H{
					"error":   "Exchange failed",
					"details": _iweb.Error(),
				})
				return
			}

			var _hf struct {
				APIKey     string `json:"api_key"`
				Tier       string `json:"tier"`
				CustomerID int    `json:"customer_id"`
			}
			json.NewDecoder(exchangeResp.Body).Decode(&_hf)

			if _hf.APIKey == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "Invalid or expired code",
					"message": "The authorization code is invalid or has expired.",
				})
				return
			}

			if err := rc._mz06(_hf.APIKey, _hf.Tier, _hf.CustomerID); err != nil {
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
				if err := _mbz(rc, uptime); err != nil {
					fmt.Printf("  ⚠ Heartbeat failed (non-blocking): %v\n", err)
				}
			}
		}
	}()
}

func Shutdown(rc *RuntimeContext) {
	if rc == nil || rc._4g == "" {
		return
	}
	_fa(rc)
}

func _x1(code string) (_4g string, err error) {
	resp, err := _j4("/v1/register/exchange", map[string]string{
		"authorization_code": code,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", _fku(resp)
	}

	var _hf struct {
		APIKey string `json:"api_key"`
	}
	json.NewDecoder(resp.Body).Decode(&_hf)
	if _hf.APIKey == "" {
		return "", fmt.Errorf("exchange returned empty api_key")
	}
	return _hf.APIKey, nil
}

func _u7(authCodeOrKey string) (string, error) {
	_4g, err := _x1(authCodeOrKey)
	if err == nil && _4g != "" {
		return _4g, nil
	}
	return authCodeOrKey, nil
}

func _wlr(rc *RuntimeContext, _r3j string) error {
	resp, err := _ys("/v1/activate", map[string]string{
		"instance_id": rc._sk,
		"version":     _r3j,
	}, rc._4g)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _fku(resp)
	}

	var _hf struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&_hf)

	if _hf.Status != "active" {
		return fmt.Errorf("activation returned status: %s", _hf.Status)
	}
	return nil
}

func _mbz(rc *RuntimeContext, uptimeSeconds int64) error {
	resp, err := _ys("/v1/heartbeat", map[string]any{
		"instance_id":    rc._sk,
		"uptime_seconds": uptimeSeconds,
		"version":        rc._r3j,
	}, rc._4g)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _fku(resp)
	}
	return nil
}

func _fa(rc *RuntimeContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{
		"instance_id": rc._sk,
	})

	url := _k5qk() + "/v1/deactivate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", rc._4g)
	req.Header.Set("X-Signature", _hw1c(body, rc._4g))
	_dw.Do(req)
}

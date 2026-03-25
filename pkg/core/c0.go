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

var _k1 = []byte{0xf0, 0x55, 0xb5, 0x79, 0xf1, 0x44, 0xd3, 0xd2, 0xe8, 0xd3, 0xf9, 0xad, 0x09, 0x86, 0x40, 0xae, 0xaa, 0x44, 0x70, 0x31, 0x72, 0xf5, 0x9b, 0xd6, 0x89, 0xaa, 0xa9, 0xe5, 0xee, 0x58, 0x1b, 0x4f, 0x16, 0x66, 0xe9, 0xe9, 0xdb, 0xac, 0x9b, 0xe5, 0xf7, 0x54}
var _k0 = []byte{0x98, 0x21, 0xc1, 0x09, 0x82, 0x7e, 0xfc, 0xfd, 0x84, 0xba, 0x9a, 0xc8, 0x67, 0xf5, 0x25, 0x80, 0xcf, 0x32, 0x1f, 0x5d, 0x07, 0x81, 0xf2, 0xb9, 0xe7, 0xcc, 0xc6, 0x90, 0x80, 0x3c, 0x7a, 0x3b, 0x7f, 0x09, 0x87, 0xc7, 0xb8, 0xc3, 0xf6, 0xcb, 0x95, 0x26}

var (
	_f1g string
	_08    string
)

func _040() string {
	if _f1g != "" && _08 != "" {
		return _yg(_f1g, _08)
	}
	parts := [...]string{"h", "tt", "ps", "://", "li", "ce", "nse", ".", "ev", "ol", "ut", "io", "nf", "ou", "nd", "at", "io", "n.", "co", "m.", "br"}
	var s string
	for _, p := range parts {
		s += p
	}
	return s
}

func _yg(enc, key string) string {
	encBytes := _rw(enc)
	keyBytes := _rw(key)
	if len(keyBytes) == 0 {
		return ""
	}
	out := make([]byte, len(encBytes))
	for i, b := range encBytes {
		out[i] = b ^ keyBytes[i%len(keyBytes)]
	}
	return string(out)
}

func _rw(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b[i/2] = _rp(s[i])<<4 | _rp(s[i+1])
	}
	return b
}

func _rp(c byte) byte {
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

var _q7h = &http.Client{Timeout: 10 * time.Second}

func _gc(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func _6l(path string, payload interface{}, _6m string) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _040() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", _6m)
	req.Header.Set("X-Signature", _gc(body, _6m))

	return _q7h.Do(req)
}

func _fq0(path string) (*http.Response, error) {
	url := _040() + path
	return _q7h.Get(url)
}

func _8cs0(path string, payload interface{}) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _040() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return _q7h.Do(req)
}

func _ngm(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	var _b8g struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(b, &_b8g); err == nil {
		msg := _b8g.Message
		if msg == "" {
			msg = _b8g.Error
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

var _24 *gorm.DB

func SetDB(db *gorm.DB) {
	_24 = db
}

func MigrateDB() error {
	if _24 == nil {
		return fmt.Errorf("core: database not set, call SetDB first")
	}
	return _24.AutoMigrate(&RuntimeConfig{})
}

func _wizb(key string) (string, error) {
	if _24 == nil {
		return "", fmt.Errorf("core: database not set")
	}
	var _68uq RuntimeConfig
	_jsrz := _24.Where("key = ?", key).First(&_68uq)
	if _jsrz.Error != nil {
		return "", _jsrz.Error
	}
	return _68uq.Value, nil
}

func _kjt(key, value string) error {
	if _24 == nil {
		return fmt.Errorf("core: database not set")
	}
	var _68uq RuntimeConfig
	_jsrz := _24.Where("key = ?", key).First(&_68uq)
	if _jsrz.Error != nil {
		return _24.Create(&RuntimeConfig{Key: key, Value: value}).Error
	}
	return _24.Model(&_68uq).Update("value", value).Error
}

func _rf(key string) {
	if _24 == nil {
		return
	}
	_24.Where("key = ?", key).Delete(&RuntimeConfig{})
}

type RuntimeData struct {
	APIKey     string
	Tier       string
	CustomerID int
}

func _pjgm() (*RuntimeData, error) {
	_6m, err := _wizb(ConfigKeyAPIKey)
	if err != nil || _6m == "" {
		return nil, fmt.Errorf("no license found")
	}

	_c85, _ := _wizb(ConfigKeyTier)
	customerIDStr, _ := _wizb(ConfigKeyCustomerID)
	customerID, _ := strconv.Atoi(customerIDStr)

	return &RuntimeData{
		APIKey:     _6m,
		Tier:       _c85,
		CustomerID: customerID,
	}, nil
}

func _fkrv(rd *RuntimeData) error {
	if err := _kjt(ConfigKeyAPIKey, rd.APIKey); err != nil {
		return err
	}
	if err := _kjt(ConfigKeyTier, rd.Tier); err != nil {
		return err
	}
	if rd.CustomerID > 0 {
		if err := _kjt(ConfigKeyCustomerID, strconv.Itoa(rd.CustomerID)); err != nil {
			return err
		}
	}
	return nil
}

func _h0s2() {
	_rf(ConfigKeyAPIKey)
	_rf(ConfigKeyTier)
	_rf(ConfigKeyCustomerID)
}

func _8u0() (string, error) {
	id, err := _wizb(ConfigKeyInstanceID)
	if err == nil && len(id) == 36 {
		return id, nil
	}

	id = _05n()
	if id == "" {
		id, err = _s3()
		if err != nil {
			return "", err
		}
	}

	if err := _kjt(ConfigKeyInstanceID, id); err != nil {
		return "", err
	}
	return id, nil
}

func _05n() string {
	hostname, _ := os.Hostname()
	macAddr := _uq4()
	if hostname == "" && macAddr == "" {
		return ""
	}

	seed := hostname + "|" + macAddr
	h := make([]byte, 16)
	copy(h, []byte(seed))
	for i := 16; i < len(seed); i++ {
		h[i%16] ^= seed[i]
	}
	h[6] = (h[6] & 0x0f) | 0x40 // _jii2 4
	h[8] = (h[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func _uq4() string {
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

func _s3() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

var _tjmw atomic.Value // set during activation

func init() {
	_tjmw.Store([]byte{0})
}

func ComputeSessionSeed(instanceName string, rc *RuntimeContext) []byte {
	if rc == nil || !rc._e8s.Load() {
		return nil // Will cause panic in caller — intentional
	}
	h := sha256.New()
	h.Write([]byte(instanceName))
	h.Write([]byte(rc._6m))
	salt, _ := _tjmw.Load().([]byte)
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

func DeriveInstanceToken(_kj string, rc *RuntimeContext) string {
	if rc == nil || !rc._e8s.Load() {
		return ""
	}
	h := sha256.Sum256([]byte(_kj + rc._6m))
	return _0q(h[:8])
}

func _0q(b []byte) string {
	const _zk = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = _zk[v>>4]
		dst[i*2+1] = _zk[v&0x0f]
	}
	return string(dst)
}

func ActivateIntegrity(rc *RuntimeContext) {
	if rc == nil {
		return
	}
	h := sha256.Sum256([]byte(rc._6m + rc._kj + "ev0"))
	_tjmw.Store(h[:])
}

const (
	hbInterval = 30 * time.Minute
)

type RuntimeContext struct {
	_6m       string
	_yunx string // GLOBAL_API_KEY from .env — used as token for licensing check
	_kj   string
	_e8s       atomic.Bool
	_ix7      [32]byte // Derived from activation — required by ValidateContext
	mu           sync.RWMutex
	_am3d       string // Registration URL shown to users before activation
	_8r9s     string // Registration token for polling
	_c85         string
	_jii2      string
}

func (rc *RuntimeContext) ContextHash() [32]byte {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._ix7
}

func (rc *RuntimeContext) IsActive() bool {
	return rc._e8s.Load()
}

func (rc *RuntimeContext) RegistrationURL() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._am3d
}

func (rc *RuntimeContext) APIKey() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._6m
}

func (rc *RuntimeContext) InstanceID() string {
	return rc._kj
}

func InitializeRuntime(_c85, _jii2, _yunx string) *RuntimeContext {
	if _c85 == "" {
		_c85 = "evolution-go"
	}
	if _jii2 == "" {
		_jii2 = "unknown"
	}

	rc := &RuntimeContext{
		_c85:         _c85,
		_jii2:      _jii2,
		_yunx: _yunx,
	}

	id, err := _8u0()
	if err != nil {
		log.Fatalf("[runtime] failed to initialize instance: %v", err)
	}
	rc._kj = id

	rd, err := _pjgm()
	if err == nil && rd.APIKey != "" {
		rc._6m = rd.APIKey
		fmt.Printf("  ✓ License found: %s...%s\n", rd.APIKey[:8], rd.APIKey[len(rd.APIKey)-4:])

		rc._ix7 = sha256.Sum256([]byte(rc._6m + rc._kj))
		rc._e8s.Store(true)
		ActivateIntegrity(rc)
		fmt.Println("  ✓ License activated successfully")

		go func() {
			if err := _zim(rc, _jii2); err != nil {
				fmt.Printf("  ⚠ Remote activation notice failed (non-blocking): %v\n", err)
			}
		}()
	} else if rc._yunx != "" {
		rc._6m = rc._yunx
		if err := _zim(rc, _jii2); err == nil {
			_fkrv(&RuntimeData{APIKey: rc._yunx, Tier: _c85})
			rc._ix7 = sha256.Sum256([]byte(rc._6m + rc._kj))
			rc._e8s.Store(true)
			ActivateIntegrity(rc)
			fmt.Printf("  ✓ GLOBAL_API_KEY accepted — license saved and activated\n")
		} else {
			rc._6m = ""
			_ps28()
			rc._e8s.Store(false)
		}
	} else {
		_ps28()
		rc._e8s.Store(false)
	}

	return rc
}

func _ps28() {
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

func (rc *RuntimeContext) _7jz(authCodeOrKey, _c85 string, customerID int) error {
	_6m, err := _ns89(authCodeOrKey)
	if err != nil {
		return fmt.Errorf("key exchange failed: %w", err)
	}

	rc.mu.Lock()
	rc._6m = _6m
	rc._am3d = ""
	rc._8r9s = ""
	rc.mu.Unlock()

	if err := _fkrv(&RuntimeData{
		APIKey:     _6m,
		Tier:       _c85,
		CustomerID: customerID,
	}); err != nil {
		fmt.Printf("  ⚠ Warning: could not save license: %v\n", err)
	}

	if err := _zim(rc, rc._jii2); err != nil {
		return err
	}

	rc.mu.Lock()
	rc._ix7 = sha256.Sum256([]byte(rc._6m + rc._kj))
	rc.mu.Unlock()
	rc._e8s.Store(true)
	ActivateIntegrity(rc)

	fmt.Printf("  ✓ License activated! Key: %s...%s (_c85: %s)\n",
		_6m[:8], _6m[len(_6m)-4:], _c85)

	go func() {
		if err := _1g(rc, 0); err != nil {
			fmt.Printf("  ⚠ First heartbeat failed: %v\n", err)
		}
	}()

	return nil
}

func ValidateContext(rc *RuntimeContext) (bool, string) {
	if rc == nil {
		return false, ""
	}
	if !rc._e8s.Load() {
		return false, rc.RegistrationURL()
	}
	expected := sha256.Sum256([]byte(rc._6m + rc._kj))
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
				"instance_id": rc._kj,
			}

			rc.mu.RLock()
			if rc._6m != "" {
				resp["api_key"] = rc._6m[:8] + "..." + rc._6m[len(rc._6m)-4:]
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
			existingURL := rc._am3d
			rc.mu.RUnlock()

			if existingURL != "" {
				c.JSON(http.StatusOK, gin.H{
					"status":       "pending",
					"register_url": existingURL,
				})
				return
			}

			payload := map[string]string{
				"tier":        rc._c85,
				"version":     rc._jii2,
				"instance_id": rc._kj,
			}
			if redirectURI := c.Query("redirect_uri"); redirectURI != "" {
				payload["redirect_uri"] = redirectURI
			}

			resp, err := _8cs0("/v1/register/init", payload)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{
					"error":   "Failed to contact licensing server",
					"details": err.Error(),
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				_b8g := _ngm(resp)
				c.JSON(resp.StatusCode, gin.H{
					"error":   "Licensing server error",
					"details": _b8g.Error(),
				})
				return
			}

			var _i9i struct {
				RegisterURL string `json:"register_url"`
				Token       string `json:"token"`
			}
			json.NewDecoder(resp.Body).Decode(&_i9i)

			rc.mu.Lock()
			rc._am3d = _i9i.RegisterURL
			rc._8r9s = _i9i.Token
			rc.mu.Unlock()

			fmt.Printf("  → Registration URL: %s\n", _i9i.RegisterURL)

			c.JSON(http.StatusOK, gin.H{
				"status":       "pending",
				"register_url": _i9i.RegisterURL,
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

			exchangeResp, err := _8cs0("/v1/register/exchange", map[string]string{
				"authorization_code": code,
				"instance_id":       rc._kj,
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
				_b8g := _ngm(exchangeResp)
				c.JSON(exchangeResp.StatusCode, gin.H{
					"error":   "Exchange failed",
					"details": _b8g.Error(),
				})
				return
			}

			var _jsrz struct {
				APIKey     string `json:"api_key"`
				Tier       string `json:"tier"`
				CustomerID int    `json:"customer_id"`
			}
			json.NewDecoder(exchangeResp.Body).Decode(&_jsrz)

			if _jsrz.APIKey == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "Invalid or expired code",
					"message": "The authorization code is invalid or has expired.",
				})
				return
			}

			if err := rc._7jz(_jsrz.APIKey, _jsrz.Tier, _jsrz.CustomerID); err != nil {
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
				if err := _1g(rc, uptime); err != nil {
					fmt.Printf("  ⚠ Heartbeat failed (non-blocking): %v\n", err)
				}
			}
		}
	}()
}

func Shutdown(rc *RuntimeContext) {
	if rc == nil || rc._6m == "" {
		return
	}
	_zwu2(rc)
}

func _4j(code string) (_6m string, err error) {
	resp, err := _8cs0("/v1/register/exchange", map[string]string{
		"authorization_code": code,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", _ngm(resp)
	}

	var _jsrz struct {
		APIKey string `json:"api_key"`
	}
	json.NewDecoder(resp.Body).Decode(&_jsrz)
	if _jsrz.APIKey == "" {
		return "", fmt.Errorf("exchange returned empty api_key")
	}
	return _jsrz.APIKey, nil
}

func _ns89(authCodeOrKey string) (string, error) {
	_6m, err := _4j(authCodeOrKey)
	if err == nil && _6m != "" {
		return _6m, nil
	}
	return authCodeOrKey, nil
}

func _zim(rc *RuntimeContext, _jii2 string) error {
	resp, err := _6l("/v1/activate", map[string]string{
		"instance_id": rc._kj,
		"version":     _jii2,
	}, rc._6m)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _ngm(resp)
	}

	var _jsrz struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&_jsrz)

	if _jsrz.Status != "active" {
		return fmt.Errorf("activation returned status: %s", _jsrz.Status)
	}
	return nil
}

func _1g(rc *RuntimeContext, uptimeSeconds int64) error {
	resp, err := _6l("/v1/heartbeat", map[string]any{
		"instance_id":    rc._kj,
		"uptime_seconds": uptimeSeconds,
		"version":        rc._jii2,
	}, rc._6m)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _ngm(resp)
	}
	return nil
}

func _zwu2(rc *RuntimeContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{
		"instance_id": rc._kj,
	})

	url := _040() + "/v1/deactivate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", rc._6m)
	req.Header.Set("X-Signature", _gc(body, rc._6m))
	_q7h.Do(req)
}

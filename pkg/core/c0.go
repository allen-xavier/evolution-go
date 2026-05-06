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

var _k1 = []byte{0x67, 0x07, 0x37, 0x1c, 0x92, 0xe2, 0x06, 0x80, 0xad, 0xcd, 0x2a, 0x5c, 0x8e, 0xed, 0x27, 0x15, 0x21, 0xab, 0x45, 0xb2, 0xe4, 0x19, 0x7f, 0xb2, 0xe7, 0x23, 0x90, 0x55, 0x2d, 0x41, 0xd5, 0xd5, 0x7a, 0x66, 0x90, 0x8b, 0xe5, 0x38, 0x83, 0xd1, 0x9c, 0xcd}
var _k0 = []byte{0x0f, 0x73, 0x43, 0x6c, 0xe1, 0xd8, 0x29, 0xaf, 0xc1, 0xa4, 0x49, 0x39, 0xe0, 0x9e, 0x42, 0x3b, 0x44, 0xdd, 0x2a, 0xde, 0x91, 0x6d, 0x16, 0xdd, 0x89, 0x45, 0xff, 0x20, 0x43, 0x25, 0xb4, 0xa1, 0x13, 0x09, 0xfe, 0xa5, 0x86, 0x57, 0xee, 0xff, 0xfe, 0xbf}

var (
	_wv string
	_eins    string
)

func _kr() string {
	if _wv != "" && _eins != "" {
		return _st(_wv, _eins)
	}
	parts := [...]string{"h", "tt", "ps", "://", "li", "ce", "nse", ".", "ev", "ol", "ut", "io", "nf", "ou", "nd", "at", "io", "n.", "co", "m.", "br"}
	var s string
	for _, p := range parts {
		s += p
	}
	return s
}

func _st(enc, key string) string {
	encBytes := _dpz(enc)
	keyBytes := _dpz(key)
	if len(keyBytes) == 0 {
		return ""
	}
	out := make([]byte, len(encBytes))
	for i, b := range encBytes {
		out[i] = b ^ keyBytes[i%len(keyBytes)]
	}
	return string(out)
}

func _dpz(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b[i/2] = _rvs(s[i])<<4 | _rvs(s[i+1])
	}
	return b
}

func _rvs(c byte) byte {
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

var _75 = &http.Client{Timeout: 10 * time.Second}

func _k1g(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func _njj(path string, payload interface{}, _cq string) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _kr() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", _cq)
	req.Header.Set("X-Signature", _k1g(body, _cq))

	return _75.Do(req)
}

func _njb(path string) (*http.Response, error) {
	url := _kr() + path
	return _75.Get(url)
}

func _843(path string, payload interface{}) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _kr() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return _75.Do(req)
}

func _0xy(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	var _wwzw struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(b, &_wwzw); err == nil {
		msg := _wwzw.Message
		if msg == "" {
			msg = _wwzw.Error
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

var _1b *gorm.DB

func SetDB(db *gorm.DB) {
	_1b = db
}

func MigrateDB() error {
	if _1b == nil {
		return fmt.Errorf("core: database not set, call SetDB first")
	}
	return _1b.AutoMigrate(&RuntimeConfig{})
}

func _6qcs(key string) (string, error) {
	if _1b == nil {
		return "", fmt.Errorf("core: database not set")
	}
	var _aw4g RuntimeConfig
	_ig78 := _1b.Where("key = ?", key).First(&_aw4g)
	if _ig78.Error != nil {
		return "", _ig78.Error
	}
	return _aw4g.Value, nil
}

func _87qq(key, value string) error {
	if _1b == nil {
		return fmt.Errorf("core: database not set")
	}
	var _aw4g RuntimeConfig
	_ig78 := _1b.Where("key = ?", key).First(&_aw4g)
	if _ig78.Error != nil {
		return _1b.Create(&RuntimeConfig{Key: key, Value: value}).Error
	}
	return _1b.Model(&_aw4g).Update("value", value).Error
}

func _100(key string) {
	if _1b == nil {
		return
	}
	_1b.Where("key = ?", key).Delete(&RuntimeConfig{})
}

type RuntimeData struct {
	APIKey     string
	Tier       string
	CustomerID int
}

func _r0s() (*RuntimeData, error) {
	_cq, err := _6qcs(ConfigKeyAPIKey)
	if err != nil || _cq == "" {
		return nil, fmt.Errorf("no license found")
	}

	_0e, _ := _6qcs(ConfigKeyTier)
	customerIDStr, _ := _6qcs(ConfigKeyCustomerID)
	customerID, _ := strconv.Atoi(customerIDStr)

	return &RuntimeData{
		APIKey:     _cq,
		Tier:       _0e,
		CustomerID: customerID,
	}, nil
}

func _2khu(rd *RuntimeData) error {
	if err := _87qq(ConfigKeyAPIKey, rd.APIKey); err != nil {
		return err
	}
	if err := _87qq(ConfigKeyTier, rd.Tier); err != nil {
		return err
	}
	if rd.CustomerID > 0 {
		if err := _87qq(ConfigKeyCustomerID, strconv.Itoa(rd.CustomerID)); err != nil {
			return err
		}
	}
	return nil
}

func _5n28() {
	_100(ConfigKeyAPIKey)
	_100(ConfigKeyTier)
	_100(ConfigKeyCustomerID)
}

func _y7() (string, error) {
	id, err := _6qcs(ConfigKeyInstanceID)
	if err == nil && len(id) == 36 {
		return id, nil
	}

	id = _a7ax()
	if id == "" {
		id, err = _3k()
		if err != nil {
			return "", err
		}
	}

	if err := _87qq(ConfigKeyInstanceID, id); err != nil {
		return "", err
	}
	return id, nil
}

func _a7ax() string {
	hostname, _ := os.Hostname()
	macAddr := _m7n6()
	if hostname == "" && macAddr == "" {
		return ""
	}

	seed := hostname + "|" + macAddr
	h := make([]byte, 16)
	copy(h, []byte(seed))
	for i := 16; i < len(seed); i++ {
		h[i%16] ^= seed[i]
	}
	h[6] = (h[6] & 0x0f) | 0x40 // _vyx 4
	h[8] = (h[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func _m7n6() string {
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

func _3k() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

var _ef atomic.Value // set during activation

func init() {
	_ef.Store([]byte{0})
}

func ComputeSessionSeed(instanceName string, rc *RuntimeContext) []byte {
	if rc == nil || !rc._32q0.Load() {
		return nil // Will cause panic in caller — intentional
	}
	h := sha256.New()
	h.Write([]byte(instanceName))
	h.Write([]byte(rc._cq))
	salt, _ := _ef.Load().([]byte)
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

func DeriveInstanceToken(_rn string, rc *RuntimeContext) string {
	if rc == nil || !rc._32q0.Load() {
		return ""
	}
	h := sha256.Sum256([]byte(_rn + rc._cq))
	return _cki(h[:8])
}

func _cki(b []byte) string {
	const _uni = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = _uni[v>>4]
		dst[i*2+1] = _uni[v&0x0f]
	}
	return string(dst)
}

func ActivateIntegrity(rc *RuntimeContext) {
	if rc == nil {
		return
	}
	h := sha256.Sum256([]byte(rc._cq + rc._rn + "ev0"))
	_ef.Store(h[:])
}

const (
	hbInterval = 30 * time.Minute
)

type RuntimeContext struct {
	_cq       string
	_dz string // GLOBAL_API_KEY from .env — used as token for licensing check
	_rn   string
	_32q0       atomic.Bool
	_mvlp      [32]byte // Derived from activation — required by ValidateContext
	mu           sync.RWMutex
	_1fb       string // Registration URL shown to users before activation
	_fcnx     string // Registration token for polling
	_0e         string
	_vyx      string
	_3vw      atomic.Int64 // Messages sent since last heartbeat
	_w38      atomic.Int64 // Messages received since last heartbeat
}

var _8z atomic.Pointer[RuntimeContext]

func (rc *RuntimeContext) TrackMessage() {
	if rc != nil {
		rc._3vw.Add(1)
	}
}

func TrackMessageSent() {
	if rc := _8z.Load(); rc != nil {
		rc._3vw.Add(1)
	}
}

func TrackMessageRecv() {
	if rc := _8z.Load(); rc != nil {
		rc._w38.Add(1)
	}
}

func (rc *RuntimeContext) _68e() int64 {
	return rc._3vw.Swap(0)
}

func (rc *RuntimeContext) ContextHash() [32]byte {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._mvlp
}

func (rc *RuntimeContext) IsActive() bool {
	return rc._32q0.Load()
}

func (rc *RuntimeContext) RegistrationURL() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._1fb
}

func (rc *RuntimeContext) APIKey() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._cq
}

func (rc *RuntimeContext) InstanceID() string {
	return rc._rn
}

func InitializeRuntime(_0e, _vyx, _dz string) *RuntimeContext {
	if _0e == "" {
		_0e = "evolution-go"
	}
	if _vyx == "" {
		_vyx = "unknown"
	}

	rc := &RuntimeContext{
		_0e:         _0e,
		_vyx:      _vyx,
		_dz: _dz,
	}

	id, err := _y7()
	if err != nil {
		log.Fatalf("[runtime] failed to initialize instance: %v", err)
	}
	rc._rn = id

	rd, err := _r0s()
	if err == nil && rd.APIKey != "" {
		rc._cq = rd.APIKey
		fmt.Printf("  ✓ License found: %s...%s\n", rd.APIKey[:8], rd.APIKey[len(rd.APIKey)-4:])

		rc._mvlp = sha256.Sum256([]byte(rc._cq + rc._rn))
		rc._32q0.Store(true)
		ActivateIntegrity(rc)
		fmt.Println("  ✓ License activated successfully")

		go func() {
			if err := _2v(rc, _vyx); err != nil {
				fmt.Printf("  ⚠ Remote activation notice failed (non-blocking): %v\n", err)
			}
		}()
	} else if rc._dz != "" {
		rc._cq = rc._dz
		if err := _2v(rc, _vyx); err == nil {
			_2khu(&RuntimeData{APIKey: rc._dz, Tier: _0e})
			rc._mvlp = sha256.Sum256([]byte(rc._cq + rc._rn))
			rc._32q0.Store(true)
			ActivateIntegrity(rc)
			fmt.Printf("  ✓ GLOBAL_API_KEY accepted — license saved and activated\n")
		} else {
			rc._cq = ""
			_1mpy()
			rc._32q0.Store(false)
		}
	} else {
		_1mpy()
		rc._32q0.Store(false)
	}

	_8z.Store(rc)

	return rc
}

func _1mpy() {
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

func (rc *RuntimeContext) _p57p(authCodeOrKey, _0e string, customerID int) error {
	_cq, err := _0t(authCodeOrKey)
	if err != nil {
		return fmt.Errorf("key exchange failed: %w", err)
	}

	rc.mu.Lock()
	rc._cq = _cq
	rc._1fb = ""
	rc._fcnx = ""
	rc.mu.Unlock()

	if err := _2khu(&RuntimeData{
		APIKey:     _cq,
		Tier:       _0e,
		CustomerID: customerID,
	}); err != nil {
		fmt.Printf("  ⚠ Warning: could not save license: %v\n", err)
	}

	if err := _2v(rc, rc._vyx); err != nil {
		return err
	}

	rc.mu.Lock()
	rc._mvlp = sha256.Sum256([]byte(rc._cq + rc._rn))
	rc.mu.Unlock()
	rc._32q0.Store(true)
	ActivateIntegrity(rc)

	fmt.Printf("  ✓ License activated! Key: %s...%s (_0e: %s)\n",
		_cq[:8], _cq[len(_cq)-4:], _0e)

	go func() {
		if err := _z1(rc, 0); err != nil {
			fmt.Printf("  ⚠ First heartbeat failed: %v\n", err)
		}
	}()

	return nil
}

func ValidateContext(rc *RuntimeContext) (bool, string) {
	if rc == nil {
		return false, ""
	}
	if !rc._32q0.Load() {
		return false, rc.RegistrationURL()
	}
	expected := sha256.Sum256([]byte(rc._cq + rc._rn))
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
				"instance_id": rc._rn,
			}

			rc.mu.RLock()
			if rc._cq != "" {
				resp["api_key"] = rc._cq[:8] + "..." + rc._cq[len(rc._cq)-4:]
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
			existingURL := rc._1fb
			rc.mu.RUnlock()

			if existingURL != "" {
				c.JSON(http.StatusOK, gin.H{
					"status":       "pending",
					"register_url": existingURL,
				})
				return
			}

			payload := map[string]string{
				"tier":        rc._0e,
				"version":     rc._vyx,
				"instance_id": rc._rn,
			}
			if redirectURI := c.Query("redirect_uri"); redirectURI != "" {
				payload["redirect_uri"] = redirectURI
			}

			resp, err := _843("/v1/register/init", payload)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{
					"error":   "Failed to contact licensing server",
					"details": err.Error(),
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				_wwzw := _0xy(resp)
				c.JSON(resp.StatusCode, gin.H{
					"error":   "Licensing server error",
					"details": _wwzw.Error(),
				})
				return
			}

			var _gqm struct {
				RegisterURL string `json:"register_url"`
				Token       string `json:"token"`
			}
			json.NewDecoder(resp.Body).Decode(&_gqm)

			rc.mu.Lock()
			rc._1fb = _gqm.RegisterURL
			rc._fcnx = _gqm.Token
			rc.mu.Unlock()

			fmt.Printf("  → Registration URL: %s\n", _gqm.RegisterURL)

			c.JSON(http.StatusOK, gin.H{
				"status":       "pending",
				"register_url": _gqm.RegisterURL,
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

			exchangeResp, err := _843("/v1/register/exchange", map[string]string{
				"authorization_code": code,
				"instance_id":       rc._rn,
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
				_wwzw := _0xy(exchangeResp)
				c.JSON(exchangeResp.StatusCode, gin.H{
					"error":   "Exchange failed",
					"details": _wwzw.Error(),
				})
				return
			}

			var _ig78 struct {
				APIKey     string `json:"api_key"`
				Tier       string `json:"tier"`
				CustomerID int    `json:"customer_id"`
			}
			json.NewDecoder(exchangeResp.Body).Decode(&_ig78)

			if _ig78.APIKey == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "Invalid or expired code",
					"message": "The authorization code is invalid or has expired.",
				})
				return
			}

			if err := rc._p57p(_ig78.APIKey, _ig78.Tier, _ig78.CustomerID); err != nil {
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
				if err := _z1(rc, uptime); err != nil {
					fmt.Printf("  ⚠ Heartbeat failed (non-blocking): %v\n", err)
				}
			}
		}
	}()
}

func Shutdown(rc *RuntimeContext) {
	if rc == nil || rc._cq == "" {
		return
	}
	_3l(rc)
}

func _l91(code string) (_cq string, err error) {
	resp, err := _843("/v1/register/exchange", map[string]string{
		"authorization_code": code,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", _0xy(resp)
	}

	var _ig78 struct {
		APIKey string `json:"api_key"`
	}
	json.NewDecoder(resp.Body).Decode(&_ig78)
	if _ig78.APIKey == "" {
		return "", fmt.Errorf("exchange returned empty api_key")
	}
	return _ig78.APIKey, nil
}

func _0t(authCodeOrKey string) (string, error) {
	_cq, err := _l91(authCodeOrKey)
	if err == nil && _cq != "" {
		return _cq, nil
	}
	return authCodeOrKey, nil
}

func _2v(rc *RuntimeContext, _vyx string) error {
	resp, err := _njj("/v1/activate", map[string]string{
		"instance_id": rc._rn,
		"version":     _vyx,
	}, rc._cq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _0xy(resp)
	}

	var _ig78 struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&_ig78)

	if _ig78.Status != "active" {
		return fmt.Errorf("activation returned status: %s", _ig78.Status)
	}
	return nil
}

func _z1(rc *RuntimeContext, uptimeSeconds int64) error {
	_3vw := rc._68e()
	_w38 := rc._w38.Swap(0)

	payload := map[string]any{
		"instance_id":    rc._rn,
		"uptime_seconds": uptimeSeconds,
		"version":        rc._vyx,
	}

	if _3vw > 0 || _w38 > 0 {
		bundle := map[string]any{}
		if _3vw > 0 {
			bundle["messages_sent"] = _3vw
		}
		if _w38 > 0 {
			bundle["messages_recv"] = _w38
		}
		payload["telemetry_bundle"] = bundle
	}

	resp, err := _njj("/v1/heartbeat", payload, rc._cq)
	if err != nil {
		rc._3vw.Add(_3vw)
		rc._w38.Add(_w38)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rc._3vw.Add(_3vw)
		rc._w38.Add(_w38)
		return _0xy(resp)
	}
	return nil
}

func _3l(rc *RuntimeContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{
		"instance_id": rc._rn,
	})

	url := _kr() + "/v1/deactivate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", rc._cq)
	req.Header.Set("X-Signature", _k1g(body, rc._cq))
	_75.Do(req)
}

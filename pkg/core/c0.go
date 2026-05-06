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

var _k1 = []byte{0x36, 0x68, 0xce, 0x1e, 0x91, 0x3b, 0x41, 0x8a, 0x7f, 0x64, 0xa1, 0x1a, 0x95, 0x5d, 0xbc, 0xdb, 0xbc, 0x8d, 0xc2, 0xd1, 0x1c, 0x97, 0x22, 0x13, 0xa7, 0x94, 0xba, 0x54, 0xcb, 0x2a, 0xe2, 0xae, 0x44, 0xe5, 0xeb, 0x67, 0x4e, 0x05, 0xa8, 0x36, 0x2c, 0x8d}
var _k0 = []byte{0x5e, 0x1c, 0xba, 0x6e, 0xe2, 0x01, 0x6e, 0xa5, 0x13, 0x0d, 0xc2, 0x7f, 0xfb, 0x2e, 0xd9, 0xf5, 0xd9, 0xfb, 0xad, 0xbd, 0x69, 0xe3, 0x4b, 0x7c, 0xc9, 0xf2, 0xd5, 0x21, 0xa5, 0x4e, 0x83, 0xda, 0x2d, 0x8a, 0x85, 0x49, 0x2d, 0x6a, 0xc5, 0x18, 0x4e, 0xff}

var (
	_0w string
	_zu7    string
)

func _ct3j() string {
	if _0w != "" && _zu7 != "" {
		return _vqjf(_0w, _zu7)
	}
	parts := [...]string{"h", "tt", "ps", "://", "li", "ce", "nse", ".", "ev", "ol", "ut", "io", "nf", "ou", "nd", "at", "io", "n.", "co", "m.", "br"}
	var s string
	for _, p := range parts {
		s += p
	}
	return s
}

func _vqjf(enc, key string) string {
	encBytes := _n968(enc)
	keyBytes := _n968(key)
	if len(keyBytes) == 0 {
		return ""
	}
	out := make([]byte, len(encBytes))
	for i, b := range encBytes {
		out[i] = b ^ keyBytes[i%len(keyBytes)]
	}
	return string(out)
}

func _n968(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b[i/2] = _y3(s[i])<<4 | _y3(s[i+1])
	}
	return b
}

func _y3(c byte) byte {
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

var _hpa = &http.Client{Timeout: 10 * time.Second}

func _t6(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func _cygr(path string, payload interface{}, _yx string) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _ct3j() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", _yx)
	req.Header.Set("X-Signature", _t6(body, _yx))

	return _hpa.Do(req)
}

func _2i5j(path string) (*http.Response, error) {
	url := _ct3j() + path
	return _hpa.Get(url)
}

func _7bqv(path string, payload interface{}) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _ct3j() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return _hpa.Do(req)
}

func _cwnt(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	var _vma3 struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(b, &_vma3); err == nil {
		msg := _vma3.Message
		if msg == "" {
			msg = _vma3.Error
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

var _vomm *gorm.DB

func SetDB(db *gorm.DB) {
	_vomm = db
}

func MigrateDB() error {
	if _vomm == nil {
		return fmt.Errorf("core: database not set, call SetDB first")
	}
	return _vomm.AutoMigrate(&RuntimeConfig{})
}

func _z7oz(key string) (string, error) {
	if _vomm == nil {
		return "", fmt.Errorf("core: database not set")
	}
	var _ezt3 RuntimeConfig
	_ufo := _vomm.Where("key = ?", key).First(&_ezt3)
	if _ufo.Error != nil {
		return "", _ufo.Error
	}
	return _ezt3.Value, nil
}

func _cm(key, value string) error {
	if _vomm == nil {
		return fmt.Errorf("core: database not set")
	}
	var _ezt3 RuntimeConfig
	_ufo := _vomm.Where("key = ?", key).First(&_ezt3)
	if _ufo.Error != nil {
		return _vomm.Create(&RuntimeConfig{Key: key, Value: value}).Error
	}
	return _vomm.Model(&_ezt3).Update("value", value).Error
}

func _pnr7(key string) {
	if _vomm == nil {
		return
	}
	_vomm.Where("key = ?", key).Delete(&RuntimeConfig{})
}

type RuntimeData struct {
	APIKey     string
	Tier       string
	CustomerID int
}

func _a7cq() (*RuntimeData, error) {
	_yx, err := _z7oz(ConfigKeyAPIKey)
	if err != nil || _yx == "" {
		return nil, fmt.Errorf("no license found")
	}

	_2au4, _ := _z7oz(ConfigKeyTier)
	customerIDStr, _ := _z7oz(ConfigKeyCustomerID)
	customerID, _ := strconv.Atoi(customerIDStr)

	return &RuntimeData{
		APIKey:     _yx,
		Tier:       _2au4,
		CustomerID: customerID,
	}, nil
}

func _np(rd *RuntimeData) error {
	if err := _cm(ConfigKeyAPIKey, rd.APIKey); err != nil {
		return err
	}
	if err := _cm(ConfigKeyTier, rd.Tier); err != nil {
		return err
	}
	if rd.CustomerID > 0 {
		if err := _cm(ConfigKeyCustomerID, strconv.Itoa(rd.CustomerID)); err != nil {
			return err
		}
	}
	return nil
}

func _x702() {
	_pnr7(ConfigKeyAPIKey)
	_pnr7(ConfigKeyTier)
	_pnr7(ConfigKeyCustomerID)
}

func _stul() (string, error) {
	id, err := _z7oz(ConfigKeyInstanceID)
	if err == nil && len(id) == 36 {
		return id, nil
	}

	id = _4pj()
	if id == "" {
		id, err = _ifwz()
		if err != nil {
			return "", err
		}
	}

	if err := _cm(ConfigKeyInstanceID, id); err != nil {
		return "", err
	}
	return id, nil
}

func _4pj() string {
	hostname, _ := os.Hostname()
	macAddr := _04dy()
	if hostname == "" && macAddr == "" {
		return ""
	}

	seed := hostname + "|" + macAddr
	h := make([]byte, 16)
	copy(h, []byte(seed))
	for i := 16; i < len(seed); i++ {
		h[i%16] ^= seed[i]
	}
	h[6] = (h[6] & 0x0f) | 0x40 // _kf 4
	h[8] = (h[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func _04dy() string {
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

func _ifwz() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

var _9n atomic.Value // set during activation

func init() {
	_9n.Store([]byte{0})
}

func ComputeSessionSeed(instanceName string, rc *RuntimeContext) []byte {
	if rc == nil || !rc._tx9.Load() {
		return nil // Will cause panic in caller — intentional
	}
	h := sha256.New()
	h.Write([]byte(instanceName))
	h.Write([]byte(rc._yx))
	salt, _ := _9n.Load().([]byte)
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

func DeriveInstanceToken(_kma string, rc *RuntimeContext) string {
	if rc == nil || !rc._tx9.Load() {
		return ""
	}
	h := sha256.Sum256([]byte(_kma + rc._yx))
	return _zgj(h[:8])
}

func _zgj(b []byte) string {
	const _34x = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = _34x[v>>4]
		dst[i*2+1] = _34x[v&0x0f]
	}
	return string(dst)
}

func ActivateIntegrity(rc *RuntimeContext) {
	if rc == nil {
		return
	}
	h := sha256.Sum256([]byte(rc._yx + rc._kma + "ev0"))
	_9n.Store(h[:])
}

const (
	hbInterval = 30 * time.Minute
)

type RuntimeContext struct {
	_yx       string
	_ikqd string // GLOBAL_API_KEY from .env — used as token for licensing check
	_kma   string
	_tx9       atomic.Bool
	_67fb      [32]byte // Derived from activation — required by ValidateContext
	mu           sync.RWMutex
	_hd       string // Registration URL shown to users before activation
	_k4w     string // Registration token for polling
	_2au4         string
	_kf      string
	_rhh      atomic.Int64 // Messages sent since last heartbeat
	_3qr      atomic.Int64 // Messages received since last heartbeat
}

var _pdt atomic.Pointer[RuntimeContext]

func (rc *RuntimeContext) TrackMessage() {
	if rc != nil {
		rc._rhh.Add(1)
	}
}

func TrackMessageSent() {
	if rc := _pdt.Load(); rc != nil {
		rc._rhh.Add(1)
	}
}

func TrackMessageRecv() {
	if rc := _pdt.Load(); rc != nil {
		rc._3qr.Add(1)
	}
}

func (rc *RuntimeContext) _emr2() int64 {
	return rc._rhh.Swap(0)
}

func (rc *RuntimeContext) ContextHash() [32]byte {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._67fb
}

func (rc *RuntimeContext) IsActive() bool {
	return rc._tx9.Load()
}

func (rc *RuntimeContext) RegistrationURL() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._hd
}

func (rc *RuntimeContext) APIKey() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._yx
}

func (rc *RuntimeContext) InstanceID() string {
	return rc._kma
}

func InitializeRuntime(_2au4, _kf, _ikqd string) *RuntimeContext {
	if _2au4 == "" {
		_2au4 = "evolution-go"
	}
	if _kf == "" {
		_kf = "unknown"
	}

	rc := &RuntimeContext{
		_2au4:         _2au4,
		_kf:      _kf,
		_ikqd: _ikqd,
	}

	id, err := _stul()
	if err != nil {
		log.Fatalf("[runtime] failed to initialize instance: %v", err)
	}
	rc._kma = id

	rd, err := _a7cq()
	if err == nil && rd.APIKey != "" {
		rc._yx = rd.APIKey
		fmt.Printf("  ✓ License found: %s...%s\n", rd.APIKey[:8], rd.APIKey[len(rd.APIKey)-4:])

		rc._67fb = sha256.Sum256([]byte(rc._yx + rc._kma))
		rc._tx9.Store(true)
		ActivateIntegrity(rc)
		fmt.Println("  ✓ License activated successfully")

		go func() {
			if err := _ys(rc, _kf); err != nil {
				fmt.Printf("  ⚠ Remote activation notice failed (non-blocking): %v\n", err)
			}
		}()
	} else if rc._ikqd != "" {
		rc._yx = rc._ikqd
		if err := _ys(rc, _kf); err == nil {
			_np(&RuntimeData{APIKey: rc._ikqd, Tier: _2au4})
			rc._67fb = sha256.Sum256([]byte(rc._yx + rc._kma))
			rc._tx9.Store(true)
			ActivateIntegrity(rc)
			fmt.Printf("  ✓ GLOBAL_API_KEY accepted — license saved and activated\n")
		} else {
			rc._yx = ""
			_tw()
			rc._tx9.Store(false)
		}
	} else {
		_tw()
		rc._tx9.Store(false)
	}

	_pdt.Store(rc)

	return rc
}

func _tw() {
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

func (rc *RuntimeContext) _jf(authCodeOrKey, _2au4 string, customerID int) error {
	_yx, err := _qgld(authCodeOrKey)
	if err != nil {
		return fmt.Errorf("key exchange failed: %w", err)
	}

	rc.mu.Lock()
	rc._yx = _yx
	rc._hd = ""
	rc._k4w = ""
	rc.mu.Unlock()

	if err := _np(&RuntimeData{
		APIKey:     _yx,
		Tier:       _2au4,
		CustomerID: customerID,
	}); err != nil {
		fmt.Printf("  ⚠ Warning: could not save license: %v\n", err)
	}

	if err := _ys(rc, rc._kf); err != nil {
		return err
	}

	rc.mu.Lock()
	rc._67fb = sha256.Sum256([]byte(rc._yx + rc._kma))
	rc.mu.Unlock()
	rc._tx9.Store(true)
	ActivateIntegrity(rc)

	fmt.Printf("  ✓ License activated! Key: %s...%s (_2au4: %s)\n",
		_yx[:8], _yx[len(_yx)-4:], _2au4)

	go func() {
		if err := _gx(rc, 0); err != nil {
			fmt.Printf("  ⚠ First heartbeat failed: %v\n", err)
		}
	}()

	return nil
}

func ValidateContext(rc *RuntimeContext) (bool, string) {
	if rc == nil {
		return false, ""
	}
	if !rc._tx9.Load() {
		return false, rc.RegistrationURL()
	}
	expected := sha256.Sum256([]byte(rc._yx + rc._kma))
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
				"instance_id": rc._kma,
			}

			rc.mu.RLock()
			if rc._yx != "" {
				resp["api_key"] = rc._yx[:8] + "..." + rc._yx[len(rc._yx)-4:]
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
			existingURL := rc._hd
			rc.mu.RUnlock()

			if existingURL != "" {
				c.JSON(http.StatusOK, gin.H{
					"status":       "pending",
					"register_url": existingURL,
				})
				return
			}

			payload := map[string]string{
				"tier":        rc._2au4,
				"version":     rc._kf,
				"instance_id": rc._kma,
			}
			if redirectURI := c.Query("redirect_uri"); redirectURI != "" {
				payload["redirect_uri"] = redirectURI
			}

			resp, err := _7bqv("/v1/register/init", payload)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{
					"error":   "Failed to contact licensing server",
					"details": err.Error(),
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				_vma3 := _cwnt(resp)
				c.JSON(resp.StatusCode, gin.H{
					"error":   "Licensing server error",
					"details": _vma3.Error(),
				})
				return
			}

			var _8ucp struct {
				RegisterURL string `json:"register_url"`
				Token       string `json:"token"`
			}
			json.NewDecoder(resp.Body).Decode(&_8ucp)

			rc.mu.Lock()
			rc._hd = _8ucp.RegisterURL
			rc._k4w = _8ucp.Token
			rc.mu.Unlock()

			fmt.Printf("  → Registration URL: %s\n", _8ucp.RegisterURL)

			c.JSON(http.StatusOK, gin.H{
				"status":       "pending",
				"register_url": _8ucp.RegisterURL,
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

			exchangeResp, err := _7bqv("/v1/register/exchange", map[string]string{
				"authorization_code": code,
				"instance_id":       rc._kma,
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
				_vma3 := _cwnt(exchangeResp)
				c.JSON(exchangeResp.StatusCode, gin.H{
					"error":   "Exchange failed",
					"details": _vma3.Error(),
				})
				return
			}

			var _ufo struct {
				APIKey     string `json:"api_key"`
				Tier       string `json:"tier"`
				CustomerID int    `json:"customer_id"`
			}
			json.NewDecoder(exchangeResp.Body).Decode(&_ufo)

			if _ufo.APIKey == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "Invalid or expired code",
					"message": "The authorization code is invalid or has expired.",
				})
				return
			}

			if err := rc._jf(_ufo.APIKey, _ufo.Tier, _ufo.CustomerID); err != nil {
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
				if err := _gx(rc, uptime); err != nil {
					fmt.Printf("  ⚠ Heartbeat failed (non-blocking): %v\n", err)
				}
			}
		}
	}()
}

func Shutdown(rc *RuntimeContext) {
	if rc == nil || rc._yx == "" {
		return
	}
	_02e(rc)
}

func _34(code string) (_yx string, err error) {
	resp, err := _7bqv("/v1/register/exchange", map[string]string{
		"authorization_code": code,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", _cwnt(resp)
	}

	var _ufo struct {
		APIKey string `json:"api_key"`
	}
	json.NewDecoder(resp.Body).Decode(&_ufo)
	if _ufo.APIKey == "" {
		return "", fmt.Errorf("exchange returned empty api_key")
	}
	return _ufo.APIKey, nil
}

func _qgld(authCodeOrKey string) (string, error) {
	_yx, err := _34(authCodeOrKey)
	if err == nil && _yx != "" {
		return _yx, nil
	}
	return authCodeOrKey, nil
}

func _ys(rc *RuntimeContext, _kf string) error {
	resp, err := _cygr("/v1/activate", map[string]string{
		"instance_id": rc._kma,
		"version":     _kf,
	}, rc._yx)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _cwnt(resp)
	}

	var _ufo struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&_ufo)

	if _ufo.Status != "active" {
		return fmt.Errorf("activation returned status: %s", _ufo.Status)
	}
	return nil
}

func _gx(rc *RuntimeContext, uptimeSeconds int64) error {
	_rhh := rc._emr2()
	_3qr := rc._3qr.Swap(0)

	payload := map[string]any{
		"instance_id":    rc._kma,
		"uptime_seconds": uptimeSeconds,
		"version":        rc._kf,
	}

	if _rhh > 0 || _3qr > 0 {
		bundle := map[string]any{}
		if _rhh > 0 {
			bundle["messages_sent"] = _rhh
		}
		if _3qr > 0 {
			bundle["messages_recv"] = _3qr
		}
		payload["telemetry_bundle"] = bundle
	}

	resp, err := _cygr("/v1/heartbeat", payload, rc._yx)
	if err != nil {
		rc._rhh.Add(_rhh)
		rc._3qr.Add(_3qr)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rc._rhh.Add(_rhh)
		rc._3qr.Add(_3qr)
		return _cwnt(resp)
	}
	return nil
}

func _02e(rc *RuntimeContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{
		"instance_id": rc._kma,
	})

	url := _ct3j() + "/v1/deactivate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", rc._yx)
	req.Header.Set("X-Signature", _t6(body, rc._yx))
	_hpa.Do(req)
}

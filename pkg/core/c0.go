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

var _k1 = []byte{0x35, 0x00, 0xe8, 0xe4, 0xcd, 0x3a, 0x18, 0x8c, 0xcb, 0xf7, 0x95, 0xcc, 0xf4, 0xfe, 0x81, 0xc0, 0x32, 0x6f, 0x0e, 0x5a, 0x76, 0x55, 0xcb, 0xb4, 0xce, 0x04, 0xad, 0x98, 0xcd, 0xe8, 0x80, 0x4a, 0xd2, 0x50, 0xc1, 0x3c, 0x83, 0x52, 0xbd, 0x0d, 0x4e, 0x27}
var _k0 = []byte{0x5d, 0x74, 0x9c, 0x94, 0xbe, 0x00, 0x37, 0xa3, 0xa7, 0x9e, 0xf6, 0xa9, 0x9a, 0x8d, 0xe4, 0xee, 0x57, 0x19, 0x61, 0x36, 0x03, 0x21, 0xa2, 0xdb, 0xa0, 0x62, 0xc2, 0xed, 0xa3, 0x8c, 0xe1, 0x3e, 0xbb, 0x3f, 0xaf, 0x12, 0xe0, 0x3d, 0xd0, 0x23, 0x2c, 0x55}

var (
	_uqc1 string
	_foag    string
)

func _ab6() string {
	if _uqc1 != "" && _foag != "" {
		return _o5(_uqc1, _foag)
	}
	parts := [...]string{"h", "tt", "ps", "://", "li", "ce", "nse", ".", "ev", "ol", "ut", "io", "nf", "ou", "nd", "at", "io", "n.", "co", "m.", "br"}
	var s string
	for _, p := range parts {
		s += p
	}
	return s
}

func _o5(enc, key string) string {
	encBytes := _873u(enc)
	keyBytes := _873u(key)
	if len(keyBytes) == 0 {
		return ""
	}
	out := make([]byte, len(encBytes))
	for i, b := range encBytes {
		out[i] = b ^ keyBytes[i%len(keyBytes)]
	}
	return string(out)
}

func _873u(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b[i/2] = _kbr(s[i])<<4 | _kbr(s[i+1])
	}
	return b
}

func _kbr(c byte) byte {
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

var _pu2 = &http.Client{Timeout: 10 * time.Second}

func _2kc6(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func _bg(path string, payload interface{}, _xu string) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _ab6() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", _xu)
	req.Header.Set("X-Signature", _2kc6(body, _xu))

	return _pu2.Do(req)
}

func _220(path string) (*http.Response, error) {
	url := _ab6() + path
	return _pu2.Get(url)
}

func _q3(path string, payload interface{}) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := _ab6() + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return _pu2.Do(req)
}

func _8jfx(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	var _40wq struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(b, &_40wq); err == nil {
		msg := _40wq.Message
		if msg == "" {
			msg = _40wq.Error
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

var _as *gorm.DB

func SetDB(db *gorm.DB) {
	_as = db
}

func MigrateDB() error {
	if _as == nil {
		return fmt.Errorf("core: database not set, call SetDB first")
	}
	return _as.AutoMigrate(&RuntimeConfig{})
}

func _9n(key string) (string, error) {
	if _as == nil {
		return "", fmt.Errorf("core: database not set")
	}
	var _jbh RuntimeConfig
	_hn := _as.Where("key = ?", key).First(&_jbh)
	if _hn.Error != nil {
		return "", _hn.Error
	}
	return _jbh.Value, nil
}

func _obyk(key, value string) error {
	if _as == nil {
		return fmt.Errorf("core: database not set")
	}
	var _jbh RuntimeConfig
	_hn := _as.Where("key = ?", key).First(&_jbh)
	if _hn.Error != nil {
		return _as.Create(&RuntimeConfig{Key: key, Value: value}).Error
	}
	return _as.Model(&_jbh).Update("value", value).Error
}

func _fwl(key string) {
	if _as == nil {
		return
	}
	_as.Where("key = ?", key).Delete(&RuntimeConfig{})
}

type RuntimeData struct {
	APIKey     string
	Tier       string
	CustomerID int
}

func _i7iu() (*RuntimeData, error) {
	_xu, err := _9n(ConfigKeyAPIKey)
	if err != nil || _xu == "" {
		return nil, fmt.Errorf("no license found")
	}

	_75, _ := _9n(ConfigKeyTier)
	customerIDStr, _ := _9n(ConfigKeyCustomerID)
	customerID, _ := strconv.Atoi(customerIDStr)

	return &RuntimeData{
		APIKey:     _xu,
		Tier:       _75,
		CustomerID: customerID,
	}, nil
}

func _hm(rd *RuntimeData) error {
	if err := _obyk(ConfigKeyAPIKey, rd.APIKey); err != nil {
		return err
	}
	if err := _obyk(ConfigKeyTier, rd.Tier); err != nil {
		return err
	}
	if rd.CustomerID > 0 {
		if err := _obyk(ConfigKeyCustomerID, strconv.Itoa(rd.CustomerID)); err != nil {
			return err
		}
	}
	return nil
}

func _cy() {
	_fwl(ConfigKeyAPIKey)
	_fwl(ConfigKeyTier)
	_fwl(ConfigKeyCustomerID)
}

func _fk() (string, error) {
	id, err := _9n(ConfigKeyInstanceID)
	if err == nil && len(id) == 36 {
		return id, nil
	}

	id = _vaeq()
	if id == "" {
		id, err = _13j()
		if err != nil {
			return "", err
		}
	}

	if err := _obyk(ConfigKeyInstanceID, id); err != nil {
		return "", err
	}
	return id, nil
}

func _vaeq() string {
	hostname, _ := os.Hostname()
	macAddr := _42()
	if hostname == "" && macAddr == "" {
		return ""
	}

	seed := hostname + "|" + macAddr
	h := make([]byte, 16)
	copy(h, []byte(seed))
	for i := 16; i < len(seed); i++ {
		h[i%16] ^= seed[i]
	}
	h[6] = (h[6] & 0x0f) | 0x40 // _5zmt 4
	h[8] = (h[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

func _42() string {
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

func _13j() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

var _y6p atomic.Value // set during activation

func init() {
	_y6p.Store([]byte{0})
}

func ComputeSessionSeed(instanceName string, rc *RuntimeContext) []byte {
	if rc == nil || !rc._s6.Load() {
		return nil // Will cause panic in caller — intentional
	}
	h := sha256.New()
	h.Write([]byte(instanceName))
	h.Write([]byte(rc._xu))
	salt, _ := _y6p.Load().([]byte)
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

func DeriveInstanceToken(_1c string, rc *RuntimeContext) string {
	if rc == nil || !rc._s6.Load() {
		return ""
	}
	h := sha256.Sum256([]byte(_1c + rc._xu))
	return _81k(h[:8])
}

func _81k(b []byte) string {
	const _6xd = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = _6xd[v>>4]
		dst[i*2+1] = _6xd[v&0x0f]
	}
	return string(dst)
}

func ActivateIntegrity(rc *RuntimeContext) {
	if rc == nil {
		return
	}
	h := sha256.Sum256([]byte(rc._xu + rc._1c + "ev0"))
	_y6p.Store(h[:])
}

const (
	hbInterval = 30 * time.Minute
)

type RuntimeContext struct {
	_xu       string
	_mb string // GLOBAL_API_KEY from .env — used as token for licensing check
	_1c   string
	_s6       atomic.Bool
	_tat      [32]byte // Derived from activation — required by ValidateContext
	mu           sync.RWMutex
	_h82j       string // Registration URL shown to users before activation
	_w2h     string // Registration token for polling
	_75         string
	_5zmt      string
	_k01      atomic.Int64 // Messages sent since last heartbeat
	_ekc      atomic.Int64 // Messages received since last heartbeat
}

var _ahj atomic.Pointer[RuntimeContext]

func (rc *RuntimeContext) TrackMessage() {
	if rc != nil {
		rc._k01.Add(1)
	}
}

func TrackMessageSent() {
	if rc := _ahj.Load(); rc != nil {
		rc._k01.Add(1)
	}
}

func TrackMessageRecv() {
	if rc := _ahj.Load(); rc != nil {
		rc._ekc.Add(1)
	}
}

func (rc *RuntimeContext) _zmh() int64 {
	return rc._k01.Swap(0)
}

func (rc *RuntimeContext) ContextHash() [32]byte {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._tat
}

func (rc *RuntimeContext) IsActive() bool {
	return rc._s6.Load()
}

func (rc *RuntimeContext) RegistrationURL() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._h82j
}

func (rc *RuntimeContext) APIKey() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc._xu
}

func (rc *RuntimeContext) InstanceID() string {
	return rc._1c
}

func InitializeRuntime(_75, _5zmt, _mb string) *RuntimeContext {
	if _75 == "" {
		_75 = "evolution-go"
	}
	if _5zmt == "" {
		_5zmt = "unknown"
	}

	rc := &RuntimeContext{
		_75:         _75,
		_5zmt:      _5zmt,
		_mb: _mb,
	}

	id, err := _fk()
	if err != nil {
		log.Fatalf("[runtime] failed to initialize instance: %v", err)
	}
	rc._1c = id

	rd, err := _i7iu()
	if err == nil && rd.APIKey != "" {
		rc._xu = rd.APIKey
		fmt.Printf("  ✓ License found: %s...%s\n", rd.APIKey[:8], rd.APIKey[len(rd.APIKey)-4:])

		rc._tat = sha256.Sum256([]byte(rc._xu + rc._1c))
		rc._s6.Store(true)
		ActivateIntegrity(rc)
		fmt.Println("  ✓ License activated successfully")

		go func() {
			if err := _7s0(rc, _5zmt); err != nil {
				fmt.Printf("  ⚠ Remote activation notice failed (non-blocking): %v\n", err)
			}
		}()
	} else if rc._mb != "" {
		rc._xu = rc._mb
		if err := _7s0(rc, _5zmt); err == nil {
			_hm(&RuntimeData{APIKey: rc._mb, Tier: _75})
			rc._tat = sha256.Sum256([]byte(rc._xu + rc._1c))
			rc._s6.Store(true)
			ActivateIntegrity(rc)
			fmt.Printf("  ✓ GLOBAL_API_KEY accepted — license saved and activated\n")
		} else {
			rc._xu = ""
			_4g5k()
			rc._s6.Store(false)
		}
	} else {
		_4g5k()
		rc._s6.Store(false)
	}

	_ahj.Store(rc)

	return rc
}

func _4g5k() {
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

func (rc *RuntimeContext) _23m(authCodeOrKey, _75 string, customerID int) error {
	_xu, err := _esi(authCodeOrKey)
	if err != nil {
		return fmt.Errorf("key exchange failed: %w", err)
	}

	rc.mu.Lock()
	rc._xu = _xu
	rc._h82j = ""
	rc._w2h = ""
	rc.mu.Unlock()

	if err := _hm(&RuntimeData{
		APIKey:     _xu,
		Tier:       _75,
		CustomerID: customerID,
	}); err != nil {
		fmt.Printf("  ⚠ Warning: could not save license: %v\n", err)
	}

	if err := _7s0(rc, rc._5zmt); err != nil {
		return err
	}

	rc.mu.Lock()
	rc._tat = sha256.Sum256([]byte(rc._xu + rc._1c))
	rc.mu.Unlock()
	rc._s6.Store(true)
	ActivateIntegrity(rc)

	fmt.Printf("  ✓ License activated! Key: %s...%s (_75: %s)\n",
		_xu[:8], _xu[len(_xu)-4:], _75)

	go func() {
		if err := _k8(rc, 0); err != nil {
			fmt.Printf("  ⚠ First heartbeat failed: %v\n", err)
		}
	}()

	return nil
}

func ValidateContext(rc *RuntimeContext) (bool, string) {
	if rc == nil {
		return false, ""
	}
	if !rc._s6.Load() {
		return false, rc.RegistrationURL()
	}
	expected := sha256.Sum256([]byte(rc._xu + rc._1c))
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
				"instance_id": rc._1c,
			}

			rc.mu.RLock()
			if rc._xu != "" {
				resp["api_key"] = rc._xu[:8] + "..." + rc._xu[len(rc._xu)-4:]
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
			existingURL := rc._h82j
			rc.mu.RUnlock()

			if existingURL != "" {
				c.JSON(http.StatusOK, gin.H{
					"status":       "pending",
					"register_url": existingURL,
				})
				return
			}

			payload := map[string]string{
				"tier":        rc._75,
				"version":     rc._5zmt,
				"instance_id": rc._1c,
			}
			if redirectURI := c.Query("redirect_uri"); redirectURI != "" {
				payload["redirect_uri"] = redirectURI
			}

			resp, err := _q3("/v1/register/init", payload)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{
					"error":   "Failed to contact licensing server",
					"details": err.Error(),
				})
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				_40wq := _8jfx(resp)
				c.JSON(resp.StatusCode, gin.H{
					"error":   "Licensing server error",
					"details": _40wq.Error(),
				})
				return
			}

			var _ne struct {
				RegisterURL string `json:"register_url"`
				Token       string `json:"token"`
			}
			json.NewDecoder(resp.Body).Decode(&_ne)

			rc.mu.Lock()
			rc._h82j = _ne.RegisterURL
			rc._w2h = _ne.Token
			rc.mu.Unlock()

			fmt.Printf("  → Registration URL: %s\n", _ne.RegisterURL)

			c.JSON(http.StatusOK, gin.H{
				"status":       "pending",
				"register_url": _ne.RegisterURL,
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

			exchangeResp, err := _q3("/v1/register/exchange", map[string]string{
				"authorization_code": code,
				"instance_id":       rc._1c,
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
				_40wq := _8jfx(exchangeResp)
				c.JSON(exchangeResp.StatusCode, gin.H{
					"error":   "Exchange failed",
					"details": _40wq.Error(),
				})
				return
			}

			var _hn struct {
				APIKey     string `json:"api_key"`
				Tier       string `json:"tier"`
				CustomerID int    `json:"customer_id"`
			}
			json.NewDecoder(exchangeResp.Body).Decode(&_hn)

			if _hn.APIKey == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":   "Invalid or expired code",
					"message": "The authorization code is invalid or has expired.",
				})
				return
			}

			if err := rc._23m(_hn.APIKey, _hn.Tier, _hn.CustomerID); err != nil {
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
				if err := _k8(rc, uptime); err != nil {
					fmt.Printf("  ⚠ Heartbeat failed (non-blocking): %v\n", err)
				}
			}
		}
	}()
}

func Shutdown(rc *RuntimeContext) {
	if rc == nil || rc._xu == "" {
		return
	}
	_aj0(rc)
}

func _86df(code string) (_xu string, err error) {
	resp, err := _q3("/v1/register/exchange", map[string]string{
		"authorization_code": code,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", _8jfx(resp)
	}

	var _hn struct {
		APIKey string `json:"api_key"`
	}
	json.NewDecoder(resp.Body).Decode(&_hn)
	if _hn.APIKey == "" {
		return "", fmt.Errorf("exchange returned empty api_key")
	}
	return _hn.APIKey, nil
}

func _esi(authCodeOrKey string) (string, error) {
	_xu, err := _86df(authCodeOrKey)
	if err == nil && _xu != "" {
		return _xu, nil
	}
	return authCodeOrKey, nil
}

func _7s0(rc *RuntimeContext, _5zmt string) error {
	resp, err := _bg("/v1/activate", map[string]string{
		"instance_id": rc._1c,
		"version":     _5zmt,
	}, rc._xu)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return _8jfx(resp)
	}

	var _hn struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&_hn)

	if _hn.Status != "active" {
		return fmt.Errorf("activation returned status: %s", _hn.Status)
	}
	return nil
}

func _k8(rc *RuntimeContext, uptimeSeconds int64) error {
	_k01 := rc._zmh()
	_ekc := rc._ekc.Swap(0)

	payload := map[string]any{
		"instance_id":    rc._1c,
		"uptime_seconds": uptimeSeconds,
		"version":        rc._5zmt,
	}

	if _k01 > 0 || _ekc > 0 {
		bundle := map[string]any{}
		if _k01 > 0 {
			bundle["messages_sent"] = _k01
		}
		if _ekc > 0 {
			bundle["messages_recv"] = _ekc
		}
		payload["telemetry_bundle"] = bundle
	}

	resp, err := _bg("/v1/heartbeat", payload, rc._xu)
	if err != nil {
		rc._k01.Add(_k01)
		rc._ekc.Add(_ekc)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rc._k01.Add(_k01)
		rc._ekc.Add(_ekc)
		return _8jfx(resp)
	}
	return nil
}

func _aj0(rc *RuntimeContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(map[string]string{
		"instance_id": rc._1c,
	})

	url := _ab6() + "/v1/deactivate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", rc._xu)
	req.Header.Set("X-Signature", _2kc6(body, rc._xu))
	_pu2.Do(req)
}

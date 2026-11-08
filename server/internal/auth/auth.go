package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Vz7797/LiveTap/server/internal/store"
	"golang.org/x/crypto/argon2"
)

type Auth struct {
	s   *store.Store
	key []byte
	mu  sync.Mutex
}

func New(s *store.Store, key []byte, password string) (*Auth, error) {
	a := &Auth{s: s, key: key}
	var n int
	e := s.DB.QueryRow("SELECT count(*) FROM admin").Scan(&n)
	if e != nil {
		return nil, e
	}
	if n == 0 {
		if e = ValidatePassword(password); e != nil {
			return nil, fmt.Errorf("首次启动需设置 LIVETAP_ADMIN_INITIAL_PASSWORD：%w", e)
		}
		_, e = s.DB.Exec("INSERT INTO admin(id,password_hash,must_change) VALUES(1,?,1)", Hash(password))
	}
	return a, e
}
func ValidatePassword(p string) error {
	if len(p) < 12 || len(p) > 256 {
		return errors.New("密码须为 12～256 字节")
	}
	return nil
}
func Hash(p string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	h := argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)
	return "$argon2id$v=19$m=65536,t=3,p=2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(h)
}
func Check(p, encoded string) bool {
	v := strings.Split(encoded, "$")
	if len(v) != 6 || v[3] != "m=65536,t=3,p=2" || len(p) > 256 {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(v[4])
	if e != nil || len(salt) != 16 {
		return false
	}
	want, e := base64.RawStdEncoding.DecodeString(v[5])
	if e != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(p), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}
func Token() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func Digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }

// 管理员会话按使用续期：SessionIdle 内有操作就顺延，连续 SessionIdle 未使用才失效；
// 无论是否使用，登录满 SessionMax 后都要重新登录。续期最多每 sessionRenewEvery 写一次库。
const (
	SessionIdle       = 7 * 24 * time.Hour
	SessionMax        = 30 * 24 * time.Hour
	sessionRenewEvery = time.Hour
)

var ErrLogin = errors.New("账号或密码不正确")
var ErrLocked = errors.New("登录尝试过多，请 15 分钟后再试")

func (a *Auth) Login(ip, user, password string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if a.locked(ip, now) {
		return "", ErrLocked
	}
	var hash string
	if e := a.s.DB.QueryRow("SELECT password_hash FROM admin WHERE id=1").Scan(&hash); e != nil {
		return "", e
	}
	valid := Check(password, hash)
	if !valid || user != "admin" {
		a.failure(ip, now)
		_ = a.s.Audit("login_failed", "admin", "登录失败")
		return "", ErrLogin
	}
	_, e := a.s.DB.Exec("DELETE FROM auth_failures WHERE ip=?", ip)
	if e != nil {
		return "", e
	}
	token := Token()
	_, e = a.s.DB.Exec("INSERT INTO sessions(token_hash,expires_at,created_at) VALUES(?,?,?)", Digest(token), now.Add(SessionIdle).Unix(), now.Unix())
	if e == nil {
		_ = a.s.Audit("login", "admin", "登录成功")
	}
	return token, e
}
func (a *Auth) Session(token string) (bool, bool) { return a.session(token, time.Now()) }

// session 校验会话，有效时按使用顺延到 now+SessionIdle（不超过登录后 SessionMax）。
func (a *Auth) session(token string, now time.Time) (bool, bool) {
	if len(token) != 43 {
		return false, false
	}
	var change bool
	var expires, created int64
	digest := Digest(token)
	e := a.s.DB.QueryRow("SELECT expires_at,created_at,must_change FROM sessions CROSS JOIN admin WHERE token_hash=?", digest).Scan(&expires, &created, &change)
	if e != nil || expires <= now.Unix() {
		return false, change
	}
	if created == 0 { // 升级前登录的会话，从现在起计算最长期限
		created = now.Unix()
	}
	next := min(now.Add(SessionIdle).Unix(), time.Unix(created, 0).Add(SessionMax).Unix())
	if next-expires >= int64(sessionRenewEvery/time.Second) {
		_, _ = a.s.DB.Exec("UPDATE sessions SET expires_at=?,created_at=? WHERE token_hash=?", next, created, digest)
	}
	return true, change
}
func (a *Auth) CSRF(token string) string {
	h := hmac.New(sha256.New, a.key)
	_, _ = h.Write([]byte("csrf:" + token))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (a *Auth) Logout(token string) {
	_, _ = a.s.DB.Exec("DELETE FROM sessions WHERE token_hash=?", Digest(token))
	_ = a.s.Audit("logout", "admin", "退出登录")
}
func (a *Auth) ChangePassword(ip, token, old, next string) error {
	if e := ValidatePassword(next); e != nil {
		return e
	}
	if old == next {
		return errors.New("新密码须与当前密码不同")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.locked(ip, time.Now()) {
		return ErrLocked
	}
	var hash string
	if e := a.s.DB.QueryRow("SELECT password_hash FROM admin WHERE id=1").Scan(&hash); e != nil {
		return e
	}
	if !Check(old, hash) {
		a.failure(ip, time.Now())
		_ = a.s.Audit("password_change_failed", "admin", "旧密码验证失败")
		return ErrLogin
	}
	tx, e := a.s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("UPDATE admin SET password_hash=?,must_change=0,failures=0,locked_until=0 WHERE id=1", Hash(next)); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM sessions WHERE token_hash<>?", Digest(token)); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	_, _ = a.s.DB.Exec("DELETE FROM auth_failures WHERE ip=?", ip)
	return a.s.Audit("password_changed", "admin", "修改密码并撤销其他会话")
}

func (a *Auth) locked(ip string, now time.Time) bool {
	var failures int
	var expires int64
	_ = a.s.DB.QueryRow("SELECT failures,expires_at FROM auth_failures WHERE ip=?", ip).Scan(&failures, &expires)
	return failures >= 5 && expires > now.Unix()
}
func (a *Auth) failure(ip string, now time.Time) {
	_, _ = a.s.DB.Exec(`INSERT INTO auth_failures VALUES(?,1,?) ON CONFLICT(ip) DO UPDATE SET failures=CASE WHEN expires_at<=? THEN 1 ELSE failures+1 END,expires_at=CASE WHEN expires_at<=? THEN excluded.expires_at ELSE expires_at END`, ip, now.Add(15*time.Minute).Unix(), now.Unix(), now.Unix())
}
func (a *Auth) Cleanup(now time.Time) error {
	if _, err := a.s.DB.Exec("DELETE FROM sessions WHERE expires_at<=?", now.Unix()); err != nil {
		return err
	}
	_, err := a.s.DB.Exec("DELETE FROM auth_failures WHERE expires_at<=?", now.Unix())
	return err
}
func (a *Auth) Maintain(ctx context.Context) {
	_ = a.Cleanup(time.Now())
	tick := time.NewTicker(15 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			_ = a.Cleanup(now)
		}
	}
}

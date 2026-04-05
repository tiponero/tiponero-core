package auth

import (
	"net/http"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"
)

const sessionName = "tiponero-session"
const sessionUserKey = "user_id"
const sessionPendingTOTPKey = "pending_totp_user_id"
const sessionTOTPSecretKey = "pending_totp_secret"

type Service struct {
	store *sessions.CookieStore
}

func NewService(secret string, secureCookies bool) *Service {
	store := sessions.NewCookieStore([]byte(secret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		Secure:   secureCookies,
		SameSite: http.SameSiteLaxMode,
	}
	return &Service{store: store}
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *Service) CreateSession(w http.ResponseWriter, r *http.Request, userID string) error {
	session, _ := s.store.Get(r, sessionName)
	session.Values[sessionUserKey] = userID
	return session.Save(r, w)
}

func (s *Service) DestroySession(w http.ResponseWriter, r *http.Request) error {
	session, _ := s.store.Get(r, sessionName)
	session.Options.MaxAge = -1
	return session.Save(r, w)
}

func (s *Service) GetUserID(r *http.Request) (string, bool) {
	session, _ := s.store.Get(r, sessionName)
	userID, ok := session.Values[sessionUserKey].(string)
	return userID, ok
}

func (s *Service) SetPendingTOTP(w http.ResponseWriter, r *http.Request, userID string) error {
	session, _ := s.store.Get(r, sessionName)
	session.Values[sessionPendingTOTPKey] = userID
	return session.Save(r, w)
}

func (s *Service) GetPendingTOTP(r *http.Request) (string, bool) {
	session, _ := s.store.Get(r, sessionName)
	userID, ok := session.Values[sessionPendingTOTPKey].(string)
	return userID, ok
}

func (s *Service) ClearPendingTOTP(w http.ResponseWriter, r *http.Request) error {
	session, _ := s.store.Get(r, sessionName)
	delete(session.Values, sessionPendingTOTPKey)
	return session.Save(r, w)
}

func (s *Service) SetPendingTOTPSecret(w http.ResponseWriter, r *http.Request, secret string) error {
	session, _ := s.store.Get(r, sessionName)
	session.Values[sessionTOTPSecretKey] = secret
	return session.Save(r, w)
}

func (s *Service) GetPendingTOTPSecret(r *http.Request) (string, bool) {
	session, _ := s.store.Get(r, sessionName)
	secret, ok := session.Values[sessionTOTPSecretKey].(string)
	return secret, ok
}

func (s *Service) ClearPendingTOTPSecret(w http.ResponseWriter, r *http.Request) error {
	session, _ := s.store.Get(r, sessionName)
	delete(session.Values, sessionTOTPSecretKey)
	return session.Save(r, w)
}

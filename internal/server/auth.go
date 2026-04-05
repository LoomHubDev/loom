package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "modernc.org/sqlite"
)

// UserStore manages user accounts and authentication backed by SQLite.
type UserStore struct {
	db     *sql.DB
	secret []byte
}

// NewUserStore opens (or creates) the user database and loads (or generates) the JWT secret.
func NewUserStore(dataDir string) (*UserStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create auth dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "auth.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open auth db: %w", err)
	}

	// Pragmas for safety.
	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %s: %w", p, err)
		}
	}

	// Schema.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			username   TEXT PRIMARY KEY,
			password   TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS auth_metadata (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create auth schema: %w", err)
	}

	secret, err := loadOrCreateSecret(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &UserStore{db: db, secret: secret}, nil
}

func loadOrCreateSecret(db *sql.DB) ([]byte, error) {
	var encoded string
	err := db.QueryRow("SELECT value FROM auth_metadata WHERE key = 'jwt_secret'").Scan(&encoded)
	if err == nil {
		return base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("query jwt secret: %w", err)
	}

	// Generate a new 32-byte secret.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	encoded = base64.RawStdEncoding.EncodeToString(secret)
	if _, err := db.Exec("INSERT INTO auth_metadata (key, value) VALUES ('jwt_secret', ?)", encoded); err != nil {
		return nil, fmt.Errorf("store jwt secret: %w", err)
	}
	return secret, nil
}

// CreateUser registers a new user with a bcrypt-hashed password.
func (us *UserStore) CreateUser(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = us.db.Exec("INSERT INTO users (username, password) VALUES (?, ?)", username, string(hash))
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// Authenticate validates credentials and returns a signed JWT token.
func (us *UserStore) Authenticate(username, password string) (string, error) {
	var hash string
	err := us.db.QueryRow("SELECT password FROM users WHERE username = ?", username).Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", fmt.Errorf("invalid credentials")
	}

	return us.signToken(username)
}

// ValidateToken verifies a JWT token and returns the username.
func (us *UserStore) ValidateToken(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("malformed token")
	}

	// Verify signature.
	sigInput := parts[0] + "." + parts[1]
	expectedSig := us.hmacSign([]byte(sigInput))
	actualSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode signature: %w", err)
	}
	if !hmac.Equal(expectedSig, actualSig) {
		return "", fmt.Errorf("invalid token signature")
	}

	// Decode payload.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}

	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parse claims: %w", err)
	}

	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return "", fmt.Errorf("token expired")
	}

	if claims.Sub == "" {
		return "", fmt.Errorf("empty subject")
	}

	return claims.Sub, nil
}

func (us *UserStore) signToken(username string) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	claims := map[string]any{
		"sub": username,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	sigInput := header + "." + payload
	sig := base64.RawURLEncoding.EncodeToString(us.hmacSign([]byte(sigInput)))

	return sigInput + "." + sig, nil
}

func (us *UserStore) hmacSign(data []byte) []byte {
	mac := hmac.New(sha256.New, us.secret)
	mac.Write(data)
	return mac.Sum(nil)
}

// Close closes the user store database.
func (us *UserStore) Close() {
	us.db.Close()
}

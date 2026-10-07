package profile

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"amux-accounts/pkg/auth"
	"amux-accounts/pkg/types"

	"golang.org/x/crypto/scrypt"
)

const ExportMagic = "AMEXP1."

type PortableProfile struct {
	Tool    string               `json:"tool"`
	Name    string               `json:"name"`
	Account string               `json:"account"`
	Saved   time.Time            `json:"saved"`
	Entries []types.ProfileEntry `json:"entries"`
}

type PortableBundle struct {
	Version  int               `json:"v"`
	Exported time.Time         `json:"exported"`
	Profiles []PortableProfile `json:"profiles"`
}

func DeriveKey(pass, salt []byte) ([]byte, error) {
	return scrypt.Key(pass, salt, 1<<15, 8, 1, 32)
}

func CollectBundle(wantTool string, wantNames map[string]bool) PortableBundle {
	var b PortableBundle
	b.Version = 1
	b.Exported = time.Now()
	for _, tn := range ToolNames(LoadConfig()) {
		if wantTool != "" && tn != wantTool {
			continue
		}
		for _, m := range ListProfiles(tn) {
			if strings.HasPrefix(m.Name, "_") {
				continue
			}
			if len(wantNames) > 0 && !wantNames[m.Name] {
				continue
			}
			b.Profiles = append(b.Profiles, PortableProfile{
				Tool:    tn,
				Name:    m.Name,
				Account: m.Account,
				Saved:   m.Saved,
				Entries: LoadProfileEntries(tn, m.Name),
			})
		}
	}
	return b
}

// SealBundle gzips + AES-256-GCM encrypts the bundle. When saltInline is true
// the key is passphrase-derived and a fresh salt is prepended; otherwise the
// caller's key is used as-is (16 zero bytes stand in for the salt slot).
func SealBundle(b PortableBundle, key []byte, saltInline bool) (string, error) {
	plain, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(plain); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}

	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if saltInline {
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return "", err
		}
		derived, err := DeriveKey(key, salt)
		if err != nil {
			return "", err
		}
		key = derived
	}
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, gz.Bytes(), nil)
	out := append(append(salt, nonce...), ct...)
	return ExportMagic + base64.URLEncoding.EncodeToString(out), nil
}

// OpenBundle decrypts a sealed blob. When saltInline the key is the raw
// passphrase and the salt is read from the blob; otherwise key is used directly.
func OpenBundle(blob string, key []byte, saltInline bool) (*PortableBundle, error) {
	blob = strings.TrimSpace(blob)
	if !strings.HasPrefix(blob, ExportMagic) {
		return nil, fmt.Errorf("invalid export format: missing magic prefix")
	}
	clean := strings.TrimPrefix(blob, ExportMagic)
	raw, err := base64.URLEncoding.DecodeString(clean)
	if err != nil {
		raw, err = base64.RawURLEncoding.DecodeString(clean)
		if err != nil {
			return nil, fmt.Errorf("decode export payload: %w", err)
		}
	}
	if len(raw) < 16+12+16 {
		return nil, fmt.Errorf("export payload too short")
	}

	salt, nonce, ct := raw[:16], raw[16:28], raw[28:]
	if saltInline {
		derived, err := DeriveKey(key, salt)
		if err != nil {
			return nil, err
		}
		key = derived
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	gz, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (wrong passphrase or key?)")
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var b PortableBundle
	if err := json.Unmarshal(plain, &b); err != nil {
		return nil, fmt.Errorf("bad bundle json: %w", err)
	}
	return &b, nil
}

func BackupDir() string { return filepath.Join(types.BaseDir(), "backups") }

// AutoBackup writes an encrypted snapshot of every profile across all tools to
// ~/.am/backups/, keyed by the machine's master key. Keeps the 20 most recent.
func AutoBackup() {
	b := CollectBundle("", nil)
	if len(b.Profiles) == 0 {
		return
	}
	dir := BackupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	key, err := auth.MasterKey()
	if err != nil {
		return
	}
	blob, err := SealBundle(b, key, false)
	if err != nil {
		return
	}
	name := time.Now().Format("2006-01-02_150405") + ".amexp"
	_ = os.WriteFile(filepath.Join(dir, name), []byte(blob+"\n"), 0o600)
	PruneBackups(dir, 20)
}

func PruneBackups(dir string, keep int) {
	des, _ := os.ReadDir(dir)
	var files []string
	for _, de := range des {
		if strings.HasSuffix(de.Name(), ".amexp") {
			files = append(files, de.Name())
		}
	}
	sort.Strings(files)
	for i := 0; i < len(files)-keep; i++ {
		_ = os.Remove(filepath.Join(dir, files[i]))
	}
}

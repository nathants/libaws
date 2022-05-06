package lib

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/avast/retry-go"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/mattn/go-isatty"
	"github.com/mikesmitty/edkey"
	"github.com/pkg/term"
	"github.com/r3labs/diff/v2"
	"golang.org/x/crypto/ssh"
)

var Commands = make(map[string]func())

type ArgsStruct interface {
	Description() string
}

var Args = make(map[string]ArgsStruct)

func SignalHandler(cancel func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		// defer func() {}()
		<-c
		Logger.Println("signal handler")
		cancel()
	}()
}

func DropLinesWithAny(s string, tokens ...string) string {
	var lines []string
outer:
	for _, line := range strings.Split(s, "\n") {
		for _, token := range tokens {
			if strings.Contains(line, token) {
				continue outer
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func Json(i interface{}) string {
	val, err := json.Marshal(i)
	if err != nil {
		panic(err)
	}
	return string(val)
}

func PformatAlways(i interface{}) string {
	val, err := json.MarshalIndent(i, "", "    ")
	if err != nil {
		panic(err)
	}
	return string(val)
}

func Pformat(i interface{}) string {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		val, err := json.Marshal(i)
		if err != nil {
			panic(err)
		}
		return string(val)
	}
	val, err := json.MarshalIndent(i, "", "    ")
	if err != nil {
		panic(err)
	}
	return string(val)
}

func Retry(ctx context.Context, fn func() error) error {
	return RetryAttempts(ctx, 9, fn)
}

// 6  attempts = 5    seconds total delay
// 7  attempts = 10   seconds total delay
// 8  attempts = 20   seconds total delay
// 9  attempts = 40   seconds total delay
// 10 attempts = 80   seconds total delay
// 11 attempts = 160  seconds total delay
// 12 attempts = 320  seconds total delay
func RetryAttempts(ctx context.Context, attempts int, fn func() error) error {
	return retry.Do(
		func() error {
			err := fn()
			if err != nil {
				return err
			}
			return nil
		},
		retry.Context(ctx),
		retry.LastErrorOnly(true),
		retry.Attempts(uint(attempts)),
		retry.Delay(150*time.Millisecond),
	)
}

func Assert(cond bool, format string, a ...interface{}) {
	if !cond {
		panic(fmt.Sprintf(format, a...))
	}
}

func Panic1(err error) {
	if err != nil {
		panic(err)
	}
}

func Panic2(x interface{}, e error) interface{} {
	if e != nil {
		Logger.Printf("fatal: %s\n", e)
		os.Exit(1)
	}
	return x
}

func Contains(parts []string, part string) bool {
	for _, p := range parts {
		if p == part {
			return true
		}
	}
	return false
}

func Chunk(xs []string, chunkSize int) [][]string {
	var xss [][]string
	xss = append(xss, []string{})
	for _, x := range xs {
		xss[len(xss)-1] = append(xss[len(xss)-1], x)
		if len(xss[len(xss)-1]) == chunkSize {
			xss = append(xss, []string{})
		}
	}
	return xss
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func StringOr(s *string, d string) string {
	if s == nil {
		return d
	}
	return *s
}

func color(code int) func(string) string {
	forced := os.Getenv("COLORS") != ""
	return func(s string) string {
		if forced || isatty.IsTerminal(os.Stdout.Fd()) {
			return fmt.Sprintf("\033[%dm%s\033[0m", code, s)
		}
		return s
	}
}

func ArnToInfraName(arn string) string {
	// arn:aws:dynamodb:region:account:name
	return strings.Split(arn, ":")[2]
}

var (
	Red     = color(31)
	Green   = color(32)
	Yellow  = color(33)
	Blue    = color(34)
	Magenta = color(35)
	Cyan    = color(36)
	White   = color(37)
)

func StringSlice(xs []*string) []string {
	var result []string
	for _, x := range xs {
		result = append(result, *x)
	}
	return result
}

func PreviewString(preview bool) string {
	if !preview {
		return ""
	}
	return "preview: "
}

func IsDigit(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func Last(parts []string) string {
	return parts[len(parts)-1]
}

func getch() (string, error) {
	t, err := term.Open("/dev/tty")
	if err != nil {
		return "", err
	}
	err = term.RawMode(t)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = t.Restore()
		_ = t.Close()
	}()
	bytes := make([]byte, 1)
	n, err := t.Read(bytes)
	if err != nil {
		return "", err
	}
	switch n {
	case 1:
		if bytes[0] == 3 {
			_ = t.Restore()
			_ = t.Close()
			os.Exit(1)
		}
		return string(bytes), nil
	default:
	}
	return "", nil
}

func PromptProceed(prompt string) error {
	fmt.Println(prompt)
	fmt.Println("proceed? y/n")
	ch, err := getch()
	if err != nil {
		return err
	}
	if ch != "y" {
		return fmt.Errorf("prompt failed")
	}
	return nil
}

func shell(format string, a ...interface{}) error {
	cmd := exec.Command("bash", "-c", fmt.Sprintf(format, a...))
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err != nil {
		Logger.Println(stderr.String())
		Logger.Println(stdout.String())
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func shellAt(dir string, format string, a ...interface{}) error {
	cmdString := fmt.Sprintf(format, a...)
	cmd := exec.Command("bash", "-c", cmdString)
	cmd.Dir = dir
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err != nil {
		Logger.Println("cmd:", cmdString)
		Logger.Println(stderr.String())
		Logger.Println(stdout.String())
		Logger.Println("error:", err)
		return err
	}
	return nil
}

func Max(i, j int) int {
	if i > j {
		return i
	}
	return j
}

func zipSha256Hex(data []byte) (map[string]*string, error) {
	results := make(map[string]*string)
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		Logger.Println("error:", err)
		return nil, err
	}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		rd, err := io.ReadAll(rc)
		if err != nil {
			Logger.Println("error:", err)
			return nil, err
		}
		results["./"+f.Name] = aws.String(sha256Short(rd))
	}
	return results, nil
}

func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func sha256Short(data []byte) string {
	return "sha256:" + sha256Hex(data)[:16]
}

func diffMapStringStringPointers(a, b map[string]*string, logPrefix string, logValues bool) (bool, error) {
	for k, v := range a {
		if v == nil {
			delete(a, k)
		}
	}
	for k, v := range b {
		if v == nil {
			delete(b, k)
		}
	}
	d, err := diff.NewDiffer()
	if err != nil {
		return false, err
	}
	changes, err := d.Diff(b, a)
	if err != nil {
		return false, err
	}
	for _, c := range changes {
		switch c.Type {
		case diff.DELETE:
			if logValues {
				Logger.Println(logPrefix, c.Type+":", c.Path[0]+" = "+fmt.Sprint(c.From))
			} else {
				Logger.Println(logPrefix, c.Type+":", c.Path[0]+" = "+sha256Short([]byte(fmt.Sprint(c.From))))
			}
		case diff.CREATE:
			if logValues {
				Logger.Println(logPrefix, c.Type+":", c.Path[0]+" = "+fmt.Sprint(c.To))
			} else {
				Logger.Println(logPrefix, c.Type+":", c.Path[0]+" = "+sha256Short([]byte(fmt.Sprint(c.To))))
			}
		case diff.UPDATE:
			if logValues {
				Logger.Println(logPrefix, c.Type+":", c.Path[0]+" = "+fmt.Sprint(c.From), "->", c.Path[0]+" = "+fmt.Sprint(c.To))
			} else {
				Logger.Println(logPrefix, c.Type+":", c.Path[0]+" = "+sha256Short([]byte(fmt.Sprint(c.From))), "->", c.Path[0]+" = "+sha256Short([]byte(fmt.Sprint(c.To))))
			}
		default:
			return false, fmt.Errorf("unknown diff type: %s", c.Type)
		}
	}
	return len(changes) > 0, nil
}

func SshKeygenEd25519() (string, string, error) {
	pubKey, privKey, _ := ed25519.GenerateKey(rand.Reader)
	publicKey, _ := ssh.NewPublicKey(pubKey)
	pemKey := &pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: edkey.MarshalED25519PrivateKey(privKey),
	}
	privKeyBytes := pem.EncodeToMemory(pemKey)
	pubKeyBytes := bytes.Trim(ssh.MarshalAuthorizedKey(publicKey), "\n")
	return string(pubKeyBytes), string(privKeyBytes), nil
}

func SshKeygenRsa() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	var privKeyBuf bytes.Buffer
	privateKeyPEM := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	err = pem.Encode(&privKeyBuf, privateKeyPEM)
	if err != nil {
		return "", "", err
	}
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}
	return string(ssh.MarshalAuthorizedKey(pub)), privKeyBuf.String(), nil
}

func FromUnixMilli(msec int64) time.Time {
	return time.Unix(msec/1e3, (msec%1e3)*1e6)
}

func NowUnixMilli() int64 {
	return time.Now().UTC().UnixNano() / int64(time.Millisecond)
}

func ToUnixMilli(t time.Time) int64 {
	return t.UnixNano() / int64(time.Millisecond)
}

func logRecover(r interface{}) {
	stack := string(debug.Stack())
	Logger.Println(r)
	Logger.Println(stack)
	Logger.Flush()
	panic(r)
}

func splitWhiteSpace(s string) []string {
	return regexp.MustCompile(` +`).Split(s, -1)
}

func splitWhiteSpaceN(s string, n int) []string {
	return regexp.MustCompile(` +`).Split(s, n)
}

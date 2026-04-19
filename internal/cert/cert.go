package cert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	caKeyPerm  = 0600
	certPerm   = 0644
	keyBitSize = 2048
)

// GenerateCertificates generates CA and leaf certificates without OpenSSL.
func GenerateCertificates(domain string, caDir string) error {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return fmt.Errorf("域名不能为空")
	}

	if err := os.MkdirAll(caDir, 0755); err != nil {
		return fmt.Errorf("创建证书目录失败: %w", err)
	}

	caKeyPath := filepath.Join(caDir, "ca.key")
	caCertPath := filepath.Join(caDir, "ca.crt")

	caKey, caCert, err := loadOrCreateCA(caKeyPath, caCertPath)
	if err != nil {
		return fmt.Errorf("准备CA证书失败: %w", err)
	}

	if err := generateServerCert(domain, caDir, caKey, caCert); err != nil {
		return fmt.Errorf("生成服务器证书失败: %w", err)
	}

	return nil
}

func loadOrCreateCA(caKeyPath, caCertPath string) (*rsa.PrivateKey, *x509.Certificate, error) {
	keyExists, err := fileExists(caKeyPath)
	if err != nil {
		return nil, nil, err
	}
	certExists, err := fileExists(caCertPath)
	if err != nil {
		return nil, nil, err
	}

	if keyExists && certExists {
		key, err := readRSAPrivateKey(caKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("读取CA私钥失败: %w", err)
		}
		cert, err := readCertificate(caCertPath)
		if err != nil {
			return nil, nil, fmt.Errorf("读取CA证书失败: %w", err)
		}
		if !cert.IsCA {
			return nil, nil, fmt.Errorf("CA证书无效：ca.crt 不是 CA 证书")
		}
		return key, cert, nil
	}

	if keyExists != certExists {
		return nil, nil, fmt.Errorf("CA文件不完整，请同时保留或删除 ca.key 与 ca.crt")
	}

	key, certDER, cert, err := createCA()
	if err != nil {
		return nil, nil, err
	}

	if err := writeRSAPrivateKey(caKeyPath, key); err != nil {
		return nil, nil, fmt.Errorf("写入CA私钥失败: %w", err)
	}
	if err := writeCertificate(caCertPath, certDER); err != nil {
		return nil, nil, fmt.Errorf("写入CA证书失败: %w", err)
	}

	return key, cert, nil
}

func createCA() (*rsa.PrivateKey, []byte, *x509.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, keyBitSize)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("生成CA私钥失败: %w", err)
	}

	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("生成CA序列号失败: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Country:            []string{"CN"},
			Province:           []string{"State"},
			Locality:           []string{"City"},
			Organization:       []string{"TraeProxy CA"},
			OrganizationalUnit: []string{"TraeProxy"},
			CommonName:         "TraeProxy Root CA",
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("生成CA证书失败: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("解析CA证书失败: %w", err)
	}

	return key, certDER, cert, nil
}

func generateServerCert(domain, caDir string, caKey *rsa.PrivateKey, caCert *x509.Certificate) error {
	keyPath := filepath.Join(caDir, fmt.Sprintf("%s.key", domain))
	certPath := filepath.Join(caDir, fmt.Sprintf("%s.crt", domain))

	key, err := rsa.GenerateKey(rand.Reader, keyBitSize)
	if err != nil {
		return fmt.Errorf("生成服务器私钥失败: %w", err)
	}

	serialNumber, err := randomSerialNumber()
	if err != nil {
		return fmt.Errorf("生成服务器证书序列号失败: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Country:            []string{"CN"},
			Province:           []string{"State"},
			Locality:           []string{"City"},
			Organization:       []string{"Organization"},
			OrganizationalUnit: []string{"Unit"},
			CommonName:         domain,
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if ip := net.ParseIP(domain); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{domain}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("签发服务器证书失败: %w", err)
	}

	if err := writeRSAPrivateKey(keyPath, key); err != nil {
		return fmt.Errorf("写入服务器私钥失败: %w", err)
	}
	if err := writeCertificate(certPath, certDER); err != nil {
		return fmt.Errorf("写入服务器证书失败: %w", err)
	}

	return nil
}

func readRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("PEM 解析失败")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("私钥格式不受支持: %w", err)
	}

	key, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("私钥类型不是 RSA")
	}

	return key, nil
}

func readCertificate(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("PEM 解析失败")
	}

	return x509.ParseCertificate(block.Bytes)
}

func writeRSAPrivateKey(path string, key *rsa.PrivateKey) error {
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	return writePEM(path, block, caKeyPerm)
}

func writeCertificate(path string, certDER []byte) error {
	block := &pem.Block{Type: "CERTIFICATE", Bytes: certDER}
	return writePEM(path, block, certPerm)
}

func writePEM(path string, block *pem.Block, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()

	return pem.Encode(f, block)
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func randomSerialNumber() (*big.Int, error) {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, max)
	if err != nil {
		return nil, err
	}
	if serial.Sign() <= 0 {
		return big.NewInt(1), nil
	}
	return serial, nil
}

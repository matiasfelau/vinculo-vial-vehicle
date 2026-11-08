package main

import (
	"encoding/base64"
	"testing"

	circl "github.com/cloudflare/circl/hpke"
	"github.com/cloudflare/circl/kem"

	"github.com/matiasfelau/vinculo-vial-vehicle/pkg/config"
)

const (
	testKEM  = circl.KEM_X25519_HKDF_SHA256
	testKDF  = circl.KDF_HKDF_SHA256
	testAEAD = circl.AEAD_AES256GCM
)

func newKeyPair(t *testing.T) (publicKeyBase64 string, privateKey kem.PrivateKey) {
	t.Helper()

	publicKey, privateKey, err := testKEM.Scheme().GenerateKeyPair()
	if err != nil {
		t.Fatalf("no se pudo generar el par de claves: %v", err)
	}
	raw, err := publicKey.MarshalBinary()
	if err != nil {
		t.Fatalf("no se pudo serializar la clave pública: %v", err)
	}

	return base64.StdEncoding.EncodeToString(raw), privateKey
}

func openWithInfo(privateKey kem.PrivateKey, ciphertext, info []byte) ([]byte, error) {
	encSize := testKEM.Scheme().CiphertextSize()
	enc, sealed := ciphertext[:encSize], ciphertext[encSize:]

	receiver, err := circl.NewSuite(testKEM, testKDF, testAEAD).NewReceiver(privateKey, info)
	if err != nil {
		return nil, err
	}
	opener, err := receiver.Setup(enc)
	if err != nil {
		return nil, err
	}

	return opener.Open(sealed, nil)
}

// El cifrador de la patente debe usar la clave de IA, el de Audit la de Audit y el
// del DNI la de Identity: cruzarlas enviaría el dato a quien no corresponde.
func TestNewEncryptorsCadaCifradorUsaLaClaveDeSuDestinatario(t *testing.T) {
	identityPublic, identityPrivate := newKeyPair(t)
	iaPublic, iaPrivate := newKeyPair(t)
	auditPublic, auditPrivate := newKeyPair(t)
	cfg := &config.Config{IdentityHPKEPublicKey: identityPublic, IAHPKEPublicKey: iaPublic, AuditPublicKey: auditPublic}

	enc, err := newEncryptors(cfg)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	dni, plate := enc.dni, enc.plate

	auditInfo := []byte("vv-audit:pat:3f2b8c1e-0000-4000-8000-000000000001")
	sealedAudit, err := enc.audit.EncryptWithInfo([]byte("AB123CD"), auditInfo)
	if err != nil {
		t.Fatalf("no se pudo cifrar la patente hacia Audit: %v", err)
	}
	if _, err := openWithInfo(auditPrivate, sealedAudit, auditInfo); err != nil {
		t.Fatalf("la patente debía descifrarse con la clave de Audit: %v", err)
	}
	for nombre, private := range map[string]kem.PrivateKey{"IA": iaPrivate, "Identity": identityPrivate} {
		if _, err := openWithInfo(private, sealedAudit, auditInfo); err == nil {
			t.Fatalf("la patente sellada hacia Audit no debía descifrarse con la clave de %s", nombre)
		}
	}

	info := []byte("3f2b8c1e-0000-4000-8000-000000000001")
	sealedPlate, err := plate.EncryptWithInfo([]byte("AB123CD"), info)
	if err != nil {
		t.Fatalf("no se pudo cifrar la patente: %v", err)
	}
	if _, err := openWithInfo(iaPrivate, sealedPlate, info); err != nil {
		t.Fatalf("la patente debía descifrarse con la clave de IA: %v", err)
	}
	if _, err := openWithInfo(identityPrivate, sealedPlate, info); err == nil {
		t.Fatal("la patente no debía descifrarse con la clave de Identity")
	}
	if _, err := openWithInfo(auditPrivate, sealedPlate, info); err == nil {
		t.Fatal("la patente sellada hacia IA no debía descifrarse con la clave de Audit")
	}

	sealedDNI, err := dni.Encrypt([]byte("30123456"))
	if err != nil {
		t.Fatalf("no se pudo cifrar el DNI: %v", err)
	}
	if _, err := openWithInfo(identityPrivate, sealedDNI, nil); err != nil {
		t.Fatalf("el DNI debía descifrarse con la clave de Identity: %v", err)
	}
	if _, err := openWithInfo(iaPrivate, sealedDNI, nil); err == nil {
		t.Fatal("el DNI no debía descifrarse con la clave de IA")
	}
}

func TestNewEncryptorsFallaConClaveInvalida(t *testing.T) {
	valid, _ := newKeyPair(t)
	// Base64 válido pero de 31 bytes: no es una clave pública X25519.
	cortaDeUnByte := base64.StdEncoding.EncodeToString(make([]byte, 31))
	largaDeUnByte := base64.StdEncoding.EncodeToString(make([]byte, 33))

	casos := map[string]*config.Config{
		"ia invalida":        {IdentityHPKEPublicKey: valid, IAHPKEPublicKey: "no-es-una-clave", AuditPublicKey: valid},
		"identity invalida":  {IdentityHPKEPublicKey: "no-es-una-clave", IAHPKEPublicKey: valid, AuditPublicKey: valid},
		"audit no es base64": {IdentityHPKEPublicKey: valid, IAHPKEPublicKey: valid, AuditPublicKey: "no-es-una-clave"},
		"audit de 31 bytes":  {IdentityHPKEPublicKey: valid, IAHPKEPublicKey: valid, AuditPublicKey: cortaDeUnByte},
		"audit de 33 bytes":  {IdentityHPKEPublicKey: valid, IAHPKEPublicKey: valid, AuditPublicKey: largaDeUnByte},
		"audit vacia":        {IdentityHPKEPublicKey: valid, IAHPKEPublicKey: valid, AuditPublicKey: ""},
	}

	for nombre, cfg := range casos {
		t.Run(nombre, func(t *testing.T) {
			if _, err := newEncryptors(cfg); err == nil {
				t.Fatal("esperaba error con una clave pública inválida")
			}
		})
	}
}

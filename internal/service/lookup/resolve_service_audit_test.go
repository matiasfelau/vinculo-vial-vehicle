package lookup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/matiasfelau/vinculo-vial-vehicle/internal/domain"
)

const testAuditKeyID = "2026-11"

func TestResolveOwnersPatEncAuditDescifraALaPatenteCanonicaAtadaAlAlertIDYAlCampo(t *testing.T) {
	pendingRef := uuid.New()
	casos := map[string]struct {
		owners     []uuid.UUID
		pendingRef *uuid.UUID
	}{
		"con dueños":    {owners: []uuid.UUID{uuid.New(), uuid.New()}},
		"sin dueños":    {owners: []uuid.UUID{}},
		"con pendiente": {owners: []uuid.UUID{}, pendingRef: &pendingRef},
	}

	for nombre, caso := range casos {
		t.Run(nombre, func(t *testing.T) {
			iaPublic, _ := newKeyPair(t)
			auditPublic, auditPrivate := newKeyPair(t)
			alertID := uuid.New()
			repo := &fakeRepository{owners: caso.owners, pendingRef: caso.pendingRef}
			// La patente llega con minúsculas y guion: Audit debe recibir la forma canónica.
			decryptor := &fakePlateDecryptor{plate: []byte("ab-123 cd")}
			service := NewResolveOwnersService(repo, decryptor, newRealEncryptor(t, iaPublic), newRealEncryptor(t, auditPublic), testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

			res, err := service.ResolveOwners(context.Background(), "criptograma", alertID, "")

			if err != nil {
				t.Fatalf("no se esperaba error: %v", err)
			}
			if res.AuditKeyID != testAuditKeyID {
				t.Fatalf("audit_key_id = %q, esperaba %q", res.AuditKeyID, testAuditKeyID)
			}
			got, err := openWithInfo(auditPrivate, res.PatEncAudit, []byte("vv-audit:pat:"+alertID.String()))
			if err != nil {
				t.Fatalf("pat_enc_audit debía descifrarse con la clave de Audit y su info: %v", err)
			}
			if string(got) != "AB123CD" {
				t.Fatalf("pat_enc_audit descifró a %q, esperaba la patente canónica %q", got, "AB123CD")
			}
		})
	}
}

func TestResolveOwnersPatEncAuditNoSeAbreConOtroInfo(t *testing.T) {
	iaPublic, _ := newKeyPair(t)
	auditPublic, auditPrivate := newKeyPair(t)
	alertID := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	service := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, newRealEncryptor(t, iaPublic), newRealEncryptor(t, auditPublic), testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	res, err := service.ResolveOwners(context.Background(), "criptograma", alertID, "")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	casos := map[string][]byte{
		"otro alert_id":        []byte("vv-audit:pat:" + uuid.NewString()),
		"info del DNI":         []byte("vv-audit:dni:" + alertID.String()),
		"alert_id sin prefijo": []byte(alertID.String()),
		"prefijo sin alert_id": []byte("vv-audit:pat:"),
		"info vacío":           nil,
	}
	for nombre, info := range casos {
		t.Run(nombre, func(t *testing.T) {
			if _, err := openWithInfo(auditPrivate, res.PatEncAudit, info); err == nil {
				t.Fatalf("pat_enc_audit no debía abrirse con %s", nombre)
			}
		})
	}
}

// IA y Audit son destinatarios independientes: cada criptograma solo lo abre la
// clave de su destinatario, y el de IA conserva su info (el alert_id a secas).
func TestResolveOwnersPatEncIAYPatEncAuditSonIndependientes(t *testing.T) {
	iaPublic, iaPrivate := newKeyPair(t)
	auditPublic, auditPrivate := newKeyPair(t)
	alertID := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	service := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, newRealEncryptor(t, iaPublic), newRealEncryptor(t, auditPublic), testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	res, err := service.ResolveOwners(context.Background(), "criptograma", alertID, "")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	iaInfo := []byte(alertID.String())
	auditInfo := []byte("vv-audit:pat:" + alertID.String())
	if got, err := openWithInfo(iaPrivate, res.PatEncIA, iaInfo); err != nil || string(got) != "AA000AA" {
		t.Fatalf("pat_enc_ia debía abrirse con la clave de IA y el alert_id: %q, %v", got, err)
	}
	if _, err := openWithInfo(iaPrivate, res.PatEncAudit, auditInfo); err == nil {
		t.Fatal("pat_enc_audit no debía abrirse con la clave de IA")
	}
	if _, err := openWithInfo(auditPrivate, res.PatEncIA, iaInfo); err == nil {
		t.Fatal("pat_enc_ia no debía abrirse con la clave de Audit")
	}
}

func TestResolveOwnersPatEncAuditCifraConInfoDeAuditYElInfoDeIANoCambia(t *testing.T) {
	alertID := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	iaEncryptor := &fakePlateEncryptor{}
	auditEncryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, iaEncryptor, auditEncryptor, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	if _, err := service.ResolveOwners(context.Background(), "criptograma", alertID, ""); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if want := "vv-audit:pat:" + alertID.String(); string(auditEncryptor.infoRecibido) != want {
		t.Fatalf("info de Audit = %q, esperaba %q", auditEncryptor.infoRecibido, want)
	}
	if string(iaEncryptor.infoRecibido) != alertID.String() {
		t.Fatalf("el info de IA no debía cambiar: %q", iaEncryptor.infoRecibido)
	}
}

func TestResolveOwnersCadaLlamadaProduceUnPatEncAuditNuevo(t *testing.T) {
	iaPublic, _ := newKeyPair(t)
	auditPublic, _ := newKeyPair(t)
	alertID := uuid.New()
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	iaEncryptor, auditEncryptor := newRealEncryptor(t, iaPublic), newRealEncryptor(t, auditPublic)

	a, err := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, iaEncryptor, auditEncryptor, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL).
		ResolveOwners(context.Background(), "criptograma", alertID, "")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	b, err := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: []byte("AA000AA")}, iaEncryptor, auditEncryptor, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL).
		ResolveOwners(context.Background(), "criptograma", alertID, "")
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	if a.PatEncAudit == b.PatEncAudit {
		t.Fatal("pat_enc_audit no debía repetirse entre llamadas")
	}
}

func TestResolveOwnersErrorDeCifradoDeAuditEsInternoYSinRespuestaParcial(t *testing.T) {
	for nombre, owners := range map[string][]uuid.UUID{"con dueños": {uuid.New()}, "sin dueños": {}} {
		t.Run(nombre, func(t *testing.T) {
			pendingRef := uuid.New()
			repo := &fakeRepository{owners: owners, pendingRef: &pendingRef}
			plate := []byte("AA000AA")
			auditEncryptor := &fakePlateEncryptor{err: errors.New("fallo de cifrado de audit")}
			service := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: plate}, &fakePlateEncryptor{}, auditEncryptor, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

			res, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), "")

			if err == nil {
				t.Fatal("esperaba un error ante el fallo de cifrado hacia Audit")
			}
			for _, domainErr := range []error{domain.ErrInvalidPlate, domain.ErrInvalidCryptogram, domain.ErrRepositoryUnavailable} {
				if errors.Is(err, domainErr) {
					t.Fatalf("el fallo de cifrado no debía mapear a un error de dominio (%v): debe ser interno", domainErr)
				}
			}
			if strings.Contains(err.Error(), "AA000AA") {
				t.Fatalf("el error no debía contener la patente: %v", err)
			}
			if res.OwnerIDs != nil || res.PatEncIA != "" || res.PatEncAudit != "" || res.AuditKeyID != "" || res.PendingRef != nil {
				t.Fatal("ante un error de cifrado no debía devolverse una respuesta parcial")
			}
			assertWiped(t, "la patente en claro", plate)
			assertWiped(t, "la patente canónica enviada a Audit", auditEncryptor.plaintext)
		})
	}
}

func TestResolveOwnersWipeaLaPatenteCanonicaDeAudit(t *testing.T) {
	repo := &fakeRepository{owners: []uuid.UUID{uuid.New()}}
	plate := []byte("AA000AA")
	auditEncryptor := &fakePlateEncryptor{}
	service := NewResolveOwnersService(repo, &fakePlateDecryptor{plate: plate}, &fakePlateEncryptor{}, auditEncryptor, testAuditKeyID, []byte("secreto"), []byte("secreto-dedup"), testPendingTTL)

	if _, err := service.ResolveOwners(context.Background(), "criptograma", uuid.New(), ""); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}

	assertWiped(t, "la patente en claro", plate)
	assertWiped(t, "la patente canónica enviada a Audit", auditEncryptor.plaintext)
}

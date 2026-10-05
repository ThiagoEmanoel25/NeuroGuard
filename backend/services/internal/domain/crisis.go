package domain

import "time"

// ─── Crise ──────────────────────────────────────────────────────────────────

// CrisisStatus é a fase de uma crise. String (não iota) porque vai e volta do
// Postgres como text, igual a Role.
type CrisisStatus string

const (
	StatusAura   CrisisStatus = "aura"      // pródromo: antecede a crise
	StatusActive CrisisStatus = "ativa"     // crise em curso
	StatusClosed CrisisStatus = "encerrada" // terminal
)

// allowed é a fonte única de "quem pode ir para onde" — uma tabela em vez de
// ifs espalhados pelos handlers: dá para ler, testar e documentar.
//
// O resgate não aparece aqui porque não é fase: é eixo ortogonal
// (rescuer_id + rescue_confirmed_at). O socorrista é avisado na aura e pode
// confirmar antes de a crise começar, então "resgate" acontece em mais de uma
// fase — e o que acontece em mais de uma fase é atributo, não estado.
//
// Toda fase existente é chave aqui, inclusive a terminal com lista vazia:
// é isso que faz IsValid e CanGoTo lerem da mesma tabela.
var allowed = map[CrisisStatus][]CrisisStatus{
	StatusAura:   {StatusActive, StatusClosed}, // encerrar direto = alarme falso
	StatusActive: {StatusClosed},
	StatusClosed: {}, // terminal: nenhuma saída
}

// CanGoTo informa se a transição de s para to é permitida.
// Status desconhecido não abre transição alguma.
func (s CrisisStatus) CanGoTo(to CrisisStatus) bool {
	for _, dst := range allowed[s] {
		if dst == to {
			return true
		}
	}
	return false
}

// IsValid informa se s é uma fase conhecida.
func (s CrisisStatus) IsValid() bool {
	_, ok := allowed[s]
	return ok
}

// Crisis é o registro de uma crise. Campos de ponteiro são os anuláveis no
// banco: distinguir "não aconteceu" de "aconteceu no zero value" importa aqui,
// porque time.Time zero é uma data válida.
type Crisis struct {
	ID        string
	PatientID string
	Status    CrisisStatus
	AuraAt    time.Time
	// Chave de idempotência do cliente. Nula quando o cliente não manda.
	ClientEventID *string

	// Resgate: eixo ortogonal às fases. Os dois são preenchidos juntos
	// (constraint crises_rescuer_coherent).
	RescuerID         *string
	RescueConfirmedAt *time.Time

	ActiveAt    *time.Time
	ClosedAt    *time.Time
	CloseReason *string
}

// Rescued informa se alguém já assumiu o resgate.
func (c *Crisis) Rescued() bool { return c.RescuerID != nil }

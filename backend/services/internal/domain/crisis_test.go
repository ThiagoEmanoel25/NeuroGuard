package domain

import "testing"

// Matriz completa origem x destino. Exaustiva de propósito: transição nova
// obriga a mexer aqui, então ninguém acrescenta uma seta sem decidir o resto.
//
// Fases: aura -> ativa -> encerrada, mais aura -> encerrada (alarme falso).
// O resgate NÃO é estado: é eixo ortogonal (rescuer_id, rescue_confirmed_at),
// porque o socorrista é avisado na aura e pode confirmar antes de a crise começar.
func TestCanGoTo(t *testing.T) {
	tests := []struct {
		from CrisisStatus
		to   CrisisStatus
		want bool
	}{
		// Da aura: começa a crise, ou encerra direto (alarme falso).
		{StatusAura, StatusActive, true},
		{StatusAura, StatusClosed, true},
		{StatusAura, StatusAura, false},

		// Da crise ativa: só encerrar. Não se volta para aura.
		{StatusActive, StatusClosed, true},
		{StatusActive, StatusAura, false},
		{StatusActive, StatusActive, false},

		// Encerrada é terminal: nenhuma saída. É o que impede confirmar
		// resgate ou reabrir uma crise já fechada.
		{StatusClosed, StatusAura, false},
		{StatusClosed, StatusActive, false},
		{StatusClosed, StatusClosed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			if got := tt.from.CanGoTo(tt.to); got != tt.want {
				t.Errorf("%s.CanGoTo(%s) = %v, quero %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

// Status desconhecido (string vazia, valor vindo de um banco corrompido ou de
// um JSON malformado) não pode abrir nenhuma transição.
func TestCanGoToFromUnknownStatus(t *testing.T) {
	var zero CrisisStatus // ""

	for _, to := range []CrisisStatus{StatusAura, StatusActive, StatusClosed} {
		if zero.CanGoTo(to) {
			t.Errorf("status vazio não deveria permitir transição para %s", to)
		}
	}
	if StatusAura.CanGoTo("resgate_confirmado") {
		t.Error("resgate não é estado: não deveria ser destino válido")
	}
}

func TestCrisisStatusIsValid(t *testing.T) {
	valid := []CrisisStatus{StatusAura, StatusActive, StatusClosed}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("%s deveria ser válido", s)
		}
	}

	// Os valores inválidos incluem 'resgate_confirmado' de propósito: era um
	// estado no desenho antigo e não pode voltar por engano.
	for _, s := range []CrisisStatus{"", "aberta", "resgate_confirmado", "ATIVA"} {
		if s.IsValid() {
			t.Errorf("%q não deveria ser válido", s)
		}
	}
}

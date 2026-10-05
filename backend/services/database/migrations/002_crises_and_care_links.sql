-- Vínculo paciente <-> cuidador. N:M entre users, com atributos próprios
-- (tipo, ordem de notificação, consentimento), o que o torna entidade e não
-- só um par de ids.
CREATE TABLE care_links (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    patient_id   uuid NOT NULL REFERENCES users(id),
    caregiver_id uuid NOT NULL REFERENCES users(id),
    kind         text NOT NULL CHECK (kind IN ('emergency_contact', 'responsible_doctor')),
    -- Ordem de acionamento na Fase 2: 1 é chamado primeiro.
    priority     smallint NOT NULL DEFAULT 1 CHECK (priority >= 1),
    -- O vínculo expõe dado de saúde, então nasce pending: só vale com aceite.
    -- Revogação guarda a data em vez de apagar a linha.
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'revoked')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    accepted_at  timestamptz,
    revoked_at   timestamptz,
    CONSTRAINT care_links_not_self CHECK (patient_id <> caregiver_id),
    CONSTRAINT care_links_unique   UNIQUE (patient_id, caregiver_id),
    CONSTRAINT care_links_accepted_coherent CHECK ((status = 'pending') = (accepted_at IS NULL)),
    CONSTRAINT care_links_revoked_coherent  CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);

-- Serve à checagem de vínculo do ReBAC ("este cuidador atende este paciente?")
-- e, invertido, a "quais pacientes este cuidador atende". Parcial: só vínculo
-- ativo é consultado, então os revogados não pesam no índice.
CREATE INDEX care_links_caregiver_active
    ON care_links (caregiver_id, patient_id) WHERE status = 'active';

CREATE TABLE crises (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    patient_id uuid NOT NULL REFERENCES users(id),
    -- Só fases. O resgate é eixo ortogonal (rescuer_id, rescue_confirmed_at)
    -- porque o socorrista é avisado na aura e pode confirmar antes de a crise
    -- começar. Espelha internal/domain/crisis.go — mudar aqui exige mudar lá.
    status     text NOT NULL DEFAULT 'aura'
               CHECK (status IN ('aura', 'ativa', 'encerrada')),
    -- Chave de idempotência gerada pelo cliente (header Idempotency-Key).
    -- Protege o replay do wearable, que o índice de "uma crise aberta" não
    -- cobre: o reenvio pode chegar com a crise já encerrada.
    client_event_id uuid,

    rescuer_id          uuid REFERENCES users(id),
    aura_at             timestamptz NOT NULL DEFAULT now(),
    active_at           timestamptz,
    rescue_confirmed_at timestamptz,
    closed_at           timestamptz,
    close_reason        text CHECK (close_reason IN ('resolved', 'false_alarm', 'timeout')),

    -- Coerência entre campos e fase: um bug em Go não grava linha impossível.
    -- O idioma (a IS NULL) = (b IS NULL) quer dizer "os dois ou nenhum".
    CONSTRAINT crises_rescuer_coherent CHECK ((rescuer_id IS NULL) = (rescue_confirmed_at IS NULL)),
    CONSTRAINT crises_closed_coherent  CHECK ((status = 'encerrada') = (closed_at IS NOT NULL)),
    CONSTRAINT crises_close_reason_coherent CHECK (close_reason IS NULL OR status = 'encerrada'),
    -- Crise ativa tem de ter o instante em que começou. Não é igualdade porque
    -- aura -> encerrada (alarme falso) encerra sem nunca ter ficado ativa.
    CONSTRAINT crises_active_coherent CHECK (status <> 'ativa' OR active_at IS NOT NULL),
    CONSTRAINT crises_idempotency UNIQUE (patient_id, client_event_id)
);

-- Uma crise aberta por paciente: colapsa as 30 vezes que o paciente em crise
-- aperta o botão. Índice único PARCIAL — só as linhas que casam com o WHERE
-- entram na unicidade, então crise encerrada não bloqueia a próxima.
CREATE UNIQUE INDEX crises_one_open_per_patient
    ON crises (patient_id) WHERE status <> 'encerrada';

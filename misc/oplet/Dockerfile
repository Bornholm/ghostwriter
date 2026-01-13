FROM alpine:3.23

ARG TARGETPLATFORM

RUN apk add \
    ca-certificates \
    openssl \
    gcompat \
    bash \
  && update-ca-certificates

COPY $TARGETPLATFORM/ghostwriter /usr/local/bin/ghostwriter

COPY --chmod=+x ./misc/oplet/run.sh /usr/local/bin/run.sh

LABEL io.oplet.task.meta.name="Recherche & Rédaction"
LABEL io.oplet.task.meta.description="Générer un article en effectuant des recherches Internet sur un sujet donné"
LABEL io.oplet.task.meta.author="Bornholm"
LABEL io.oplet.task.meta.url="https://github.com/Bornholm/ghostwriter"

# Oplet inputs

LABEL io.oplet.task.inputs.GHOSTWRITER_SUBJECT.label="Sujet"
LABEL io.oplet.task.inputs.GHOSTWRITER_SUBJECT.type="text"
LABEL io.oplet.task.inputs.GHOSTWRITER_SUBJECT.description="Le sujet sur lequel effectuer des recherches et écrire un article"
LABEL io.oplet.task.inputs.GHOSTWRITER_SUBJECT.required="true"
LABEL io.oplet.task.inputs.GHOSTWRITER_SUBJECT.tags="multiline"

LABEL io.oplet.task.inputs.SEARCH_DEPTH.label="Profondeur de recherche"
LABEL io.oplet.task.inputs.SEARCH_DEPTH.type="number"
LABEL io.oplet.task.inputs.SEARCH_DEPTH.description="La 'profondeur' de recherche (1: recherche simple, 2: recherche approfondie)"
LABEL io.oplet.task.inputs.SEARCH_DEPTH.tags="min:1 max:2"

LABEL io.oplet.task.inputs.GHOSTWRITER_TARGET_WORDS.label="Nombre de mots"
LABEL io.oplet.task.inputs.GHOSTWRITER_TARGET_WORDS.type="number"
LABEL io.oplet.task.inputs.GHOSTWRITER_TARGET_WORDS.description="Le nombre de mots ciblé (par défaut 1000)"
LABEL io.oplet.task.inputs.GHOSTWRITER_TARGET_WORDS.tags="min:100 max:5000"

LABEL io.oplet.task.inputs.GHOSTWRITER_STYLE_GUIDE.label="Règles de style"
LABEL io.oplet.task.inputs.GHOSTWRITER_STYLE_GUIDE.type="text"
LABEL io.oplet.task.inputs.GHOSTWRITER_STYLE_GUIDE.description="Règles de style à appliquer à la rédaction"
LABEL io.oplet.task.inputs.GHOSTWRITER_STYLE_GUIDE.tags="multiline"

# Oplet configuration

LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_PROVIDER.type="text"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_PROVIDER.label="Fournisseur API LLM"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_PROVIDER.description="Exemple: openrouter, openai, mistral"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_PROVIDER.required="true"

LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_BASE_URL.type="text"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_BASE_URL.label="URL de base de l'API LLM"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_BASE_URL.description="Exemple: https://openrouter.ai/api/v1, https://api.openai.com/v1, https://api.mistral.ai/v1"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_BASE_URL.required="true"

LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_MODEL.type="text"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_MODEL.label="Modèle du LLM"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_MODEL.description="Exemple: google/gemini-2.5-flash"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_MODEL.required="true"

LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_API_KEY.type="secret"
LABEL io.oplet.task.config.GHOSTWRITER_CHAT_COMPLETION_API_KEY.label="Clé d'authentification API LLM"

CMD ["/usr/local/bin/run.sh"]
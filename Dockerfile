ARG RALPHEX_GO_IMAGE=ghcr.io/umputun/ralphex-go:latest
FROM ${RALPHEX_GO_IMAGE}

LABEL org.opencontainers.image.description="ralphex image with TeX Live for mreview development"

USER root
RUN apk add --no-cache \
    texlive \
    texlive-luatex \
    texlive-xetex \
    texmf-dist-latexextra \
    texmf-dist-latexrecommended \
    texmf-dist-pictures \
    texmf-dist-fontsextra \
    texmf-dist-fontsrecommended \
    texmf-dist-formatsextra \
    biber

# Leave the image user as root so the ralphex baseimage entrypoint can run
# /srv/init.sh setup, then drop privileges to app for the main command.

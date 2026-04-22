FROM ghcr.io/umputun/ralphex-go:latest

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
USER app

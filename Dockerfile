FROM ubuntu:focal

ENV DEBIAN_FRONTEND noninteractive

ADD wkhtmltopdf.deb .

RUN set -x ; \
	apt-get update \
	&& apt-get -y -q install libreoffice libreoffice-writer ure libreoffice-java-common libreoffice-core \
	 libreoffice-common openjdk-8-jre fonts-opensymbol hyphen-fr hyphen-de hyphen-en-us hyphen-it hyphen-ru \
	 fonts-dejavu fonts-dejavu-core fonts-dejavu-extra fonts-noto fonts-dustin fonts-f500 fonts-fanwood \
	 fonts-freefont-ttf fonts-liberation fonts-lmodern fonts-lyx fonts-sil-gentium fonts-texgyre fonts-tlwg-purisa \
	 ghostscript xvfb xfonts-75dpi dos2unix linux-image-extra-virtual xz-utils \
	&& apt-get -q -y remove libreoffice-gnome libreoffice-gtk3 \
	&& dpkg -i wkhtmltopdf.deb \
	&& apt-get -f install

EXPOSE 8997

RUN adduser --system --disabled-password --gecos "" --shell=/bin/bash libreoffice

ADD sofficerc /etc/libreoffice/sofficerc
VOLUME ["/tmp"]

RUN mkdir /server
WORKDIR /server
COPY app /server/server

ENTRYPOINT ["/bin/bash", "-c", "set -e && /server/server"]
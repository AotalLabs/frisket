FROM xcgd/libreoffice

RUN apt-get update && apt-get upgrade -y && apt-get -y -q install ghostscript curl xvfb xfonts-75dpi dos2unix linux-image-extra-virtual xz-utils && \
    curl "https://github.com/wkhtmltopdf/wkhtmltopdf/releases/download/0.12.5/wkhtmltox_0.12.5-1.xenial_amd64.deb" -L -o "wkhtmltopdf.deb" && \
    dpkg -i wkhtmltopdf.deb && \
	  apt-get -f install

RUN mkdir /server
WORKDIR /server
COPY app /server/server

ENTRYPOINT ["/bin/bash", "-c", "set -e && /server/server"]
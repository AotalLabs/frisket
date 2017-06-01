FROM xcgd/libreoffice

RUN apt-get update && apt-get -y -q install ghostscript curl xvfb xfonts-75dpi dos2unix linux-image-extra-virtual xz-utils && \
    curl "https://downloads.wkhtmltopdf.org/0.12/0.12.4/wkhtmltox-0.12.4_linux-generic-amd64.tar.xz" -L -o "wkhtmltopdf.tar.xz" && \
    tar -xvf "wkhtmltopdf.tar.xz" && \
    mv wkhtmltox/bin/wkhtmltopdf /usr/local/bin/wkhtmltopdf && \
	apt-get -f install

RUN mkdir /server
WORKDIR /server
COPY app /server/server

ENTRYPOINT ["/bin/bash", "-c", "set -e && /server/server"]
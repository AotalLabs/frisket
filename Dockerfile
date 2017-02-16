FROM xcgd/libreoffice

RUN apt-get update && apt-get -y -q install ghostscript wget xvfb xfonts-75dpi dos2unix linux-image-extra-virtual && \
	wget http://download.gna.org/wkhtmltopdf/0.12/0.12.2.1/wkhtmltox-0.12.2.1_linux-trusty-amd64.deb && \
	dpkg -i wkhtmltox-0.12.2.1_linux-trusty-amd64.deb && \
	apt-get -f install

RUN mkdir /server
WORKDIR /server
COPY app /server/server

ENTRYPOINT ["./server"]
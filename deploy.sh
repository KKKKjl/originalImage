#!/bin/bash
systemctl stop original_image
systemctl disable original_image

mkdir -p /etc/original_image

cp -f original_image /usr/local/bin/original_image
cp -f etc/config.json /etc/original_image/config.json
chmod +x /usr/local/bin/original_image

ln -s $(pwd)/deploy/original_image.service /etc/systemd/system/original_image.service

systemctl daemon-reload
systemctl enable original_image
systemctl start original_image

echo "original_image service deployed!"
systemctl status original_image

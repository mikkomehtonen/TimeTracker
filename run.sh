docker run -d -p 3111:8080 \
  --restart unless-stopped \
  --name timetracker \
  -v /data/timetracker:/data:rw \
   timetracker

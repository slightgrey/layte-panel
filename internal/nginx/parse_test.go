package nginx

import "testing"

const certbotConf = `
server {
    server_name blog.example.com www.blog.example.com;
    root /var/www/blog;
    index index.php;

    location / {
        try_files $uri $uri/ /index.php?$args;
    }
    location ~ \.php$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:/run/php/php8.3-fpm.sock;
    }

    listen 443 ssl; # managed by Certbot
    ssl_certificate /etc/letsencrypt/live/blog.example.com/fullchain.pem; # managed by Certbot
    ssl_certificate_key /etc/letsencrypt/live/blog.example.com/privkey.pem; # managed by Certbot
    include /etc/letsencrypt/options-ssl-nginx.conf; # managed by Certbot
}
server {
    if ($host = blog.example.com) {
        return 301 https://$host$request_uri;
    } # managed by Certbot

    listen 80;
    server_name blog.example.com www.blog.example.com;
    return 404; # managed by Certbot
}
`

func TestParseCertbot(t *testing.T) {
	ds, err := Parse(certbotConf)
	if err != nil {
		t.Fatal(err)
	}
	servers := Find(ds, "server")
	if len(servers) != 2 {
		t.Fatalf("servers = %d", len(servers))
	}
	s := servers[0].Block
	if got := First(s, "server_name").Args; len(got) != 2 || got[1] != "www.blog.example.com" {
		t.Errorf("server_name = %v", got)
	}
	if First(s, "root").Arg(0) != "/var/www/blog" {
		t.Error("root")
	}
	var pass string
	Walk(s, func(d *Directive) {
		if d.Name == "fastcgi_pass" {
			pass = d.Arg(0)
		}
	})
	if pass != "unix:/run/php/php8.3-fpm.sock" {
		t.Errorf("fastcgi_pass = %q", pass)
	}
	if l := First(s, "listen"); l.Arg(0) != "443" || l.Arg(1) != "ssl" {
		t.Errorf("listen = %v", l.Args)
	}
	// The `if` block in the second server.
	if iff := First(servers[1].Block, "if"); iff == nil || iff.Arg(0) != "($host" || len(iff.Block) != 1 {
		t.Errorf("if block = %+v", iff)
	}
}

func TestParseQuotesAndRegex(t *testing.T) {
	ds, err := Parse(`location ~* "\.(jpg|png)$" { add_header X-Test 'a;b{c}'; }
log_format main '$remote_addr "$request"';`)
	if err != nil {
		t.Fatal(err)
	}
	loc := ds[0]
	if loc.Arg(1) != `.(jpg|png)$` || loc.Block[0].Arg(1) != "a;b{c}" {
		t.Errorf("loc = %+v / %+v", loc.Args, loc.Block[0].Args)
	}
	if ds[1].Arg(1) != `$remote_addr "$request"` {
		t.Errorf("log_format = %q", ds[1].Arg(1))
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{
		"server {",
		"server { listen 80 }",
		"}",
		"listen 80",
		`root "/var/www;`,
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) should fail", src)
		}
	}
}

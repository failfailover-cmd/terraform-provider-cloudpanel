# terraform-provider-cloudpanel

Terraform-провайдер для [CloudPanel](https://www.cloudpanel.io/) на EC2.

У CloudPanel нет REST API, только `clpctl` на хосте. Провайдер ходит на хост через
AWS SSM Run Command и вызывает только три своих документа:

| Документ | Что делает |
|---|---|
| `cloudpanel-site-list` | Читает список сайтов из базы панели (`/home/clp/htdocs/app/data/db.sq3`, режим ro). |
| `cloudpanel-site-add-static` | `clpctl site:add:static`. Пароль site user генерируется на хосте и не выводится. |
| `cloudpanel-site-delete` | `clpctl site:delete --force`: удаляет сайт, его site user и файлы. |

Документы и IAM-права живут рядом с хостом в terraform_practics:
`infra/aws/ec2/cloudpanel_ssm.tf` и `infra/aws/ci-permissions/ci_cloudpanel.tf`.
Роли plan нужен только `cloudpanel-site-list`, роли apply — все три.

## Пример

```hcl
terraform {
  required_providers {
    cloudpanel = {
      source  = "failfailover-cmd/cloudpanel"
      version = "~> 0.1"
    }
  }
}

provider "cloudpanel" {
  instance_id = "i-080e387dfdb55ed8a"
  region      = "eu-central-1"
}

resource "cloudpanel_static_site" "example" {
  domain_name = "example.com"
  site_user   = "example-com"
}
```

Учётные данные AWS берутся из стандартной цепочки SDK (в CI — OIDC-роль).

## Ресурсы

`cloudpanel_static_site`:

- `domain_name` — домен в нижнем регистре, смена пересоздаёт сайт;
- `site_user` — Linux-пользователь сайта, уникальный на хосте, до 32 символов, смена пересоздаёт сайт;
- `root_directory` (computed) — каталог в `/home/<site_user>/htdocs`;
- импорт: `terraform import cloudpanel_static_site.example example.com`.

Список сайтов запрашивается один раз за запуск: refresh 60 сайтов — одна команда SSM.
Вывод SSM без S3 ограничен 24000 символами (около 200 сайтов). При достижении предела
провайдер падает с ошибкой, а не работает на обрезанном списке.

## Разработка

На хосте Go 1.22, а AWS SDK требует 1.24:

```bash
docker run --rm -v "$PWD":/src -v /root/go/pkg/mod:/go/pkg/mod -w /src golang:1.24 \
  sh -c 'go vet ./... && go test ./... && go build -o bin/terraform-provider-cloudpanel .'
```

Релиз — как у terraform-provider-flokinet: тег `v*` → GitHub Actions → GoReleaser с GPG
(секреты `GPG_PRIVATE_KEY`, `PASSPHRASE`) → Terraform Registry.

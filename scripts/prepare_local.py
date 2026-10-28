#!/usr/bin/env python3
"""Explicit, one-time local account preparation in the existing mysql container.
No production target argument, container recreation, schema migration or data reset.
"""
import os
from pathlib import Path
import secrets
import shlex
import subprocess

ROOT = Path(__file__).resolve().parents[1]
DEST = ROOT / '.tools/local.env'

def main():
    if DEST.exists():
        raise SystemExit(f'{DEST} already exists; reuse it. No accounts or data changed.')
    docker = ['docker']
    if subprocess.run(docker + ['info'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
        docker = ['sudo', '-n', 'docker']
    network = subprocess.check_output(docker + ['inspect', '--format', '{{.HostConfig.NetworkMode}}', 'mysql'], text=True).strip()
    if network != 'host':
        raise SystemExit('Expected the existing local mysql container on host networking; refusing a different topology.')
    def sql(query):
        return subprocess.check_output(docker + ['exec', '-i', 'mysql', 'sh', '-c',
            'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql --protocol=socket -uroot --batch --skip-column-names'],
            input=query, text=True)
    names = ['dexwallet_test_admin', 'dexwallet_test_http', 'dexwallet_dev']
    # Never overwrite pre-existing accounts or adopt another project's schemas.
    count = sql("SELECT COUNT(*) FROM mysql.user WHERE User IN ('dexwallet_test_admin','dexwallet_test_http','dexwallet_dev');")
    schemas = sql("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name IN ('dexwallet_test_http','dexwallet_dev');")
    if int(count.strip()) or int(schemas.strip()):
        raise SystemExit('Reserved local accounts/schemas already exist. Supply scoped test credentials explicitly; no changes made.')
    passwords = {name: secrets.token_hex(24) for name in names}
    values = {
        'DEXWALLET_TEST_ENV': 'local', 'DEXWALLET_TEST_MYSQL_ADDRESS': '127.0.0.1:3306',
        'DEXWALLET_TEST_MYSQL_DSN': f'dexwallet_test_admin:{passwords[names[0]]}@tcp(127.0.0.1:3306)/',
        'DEXWALLET_TEST_HTTP_MYSQL_DSN': f'dexwallet_test_http:{passwords[names[1]]}@tcp(127.0.0.1:3306)/dexwallet_test_http',
        'DEXWALLET_MYSQL_DSN': f'dexwallet_dev:{passwords[names[2]]}@tcp(127.0.0.1:3306)/dexwallet_dev',
    }
    DEST.parent.mkdir(exist_ok=True)
    pending = DEST.with_suffix('.env.pending')
    # Retain credentials for recovery if provisioning is interrupted. Never print them.
    with open(pending, 'x', opener=lambda p, f: os.open(p, f, 0o600)) as file:
        file.write('\n'.join('export '+k+'='+shlex.quote(v) for k,v in values.items())+'\n')
    statements = [f"CREATE USER '{n}'@'%' IDENTIFIED BY '{passwords[n]}';" for n in names]
    statements += [
        "GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX ON `dexwallet\\_contract\\_%`.* TO 'dexwallet_test_admin'@'%';",
        "CREATE DATABASE dexwallet_test_http CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;",
        "GRANT SELECT ON dexwallet_test_http.* TO 'dexwallet_test_http'@'%';",
        "CREATE DATABASE dexwallet_dev CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;",
        "GRANT SELECT, INSERT, UPDATE, DELETE ON dexwallet_dev.* TO 'dexwallet_dev'@'%';",
    ]
    sql('\n'.join(statements))
    pending.rename(DEST)
    print(f'Prepared persistent local dev/HTTP-test schemas and scoped accounts. Secrets: {DEST} (0600).')
    print('No business tables, migrations, production connections or shared service changes.')

if __name__ == '__main__':
    main()

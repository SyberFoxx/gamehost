# Manual de gamehost

Guía completa para instalar, abrir el panel interactivo (TUI) y administrar tus servidores de
Core Keeper, Project Zomboid y Don't Starve Together sin memorizar comandos.

## 1. Qué es y qué necesitas

`gamehost` es un gestor de servidores dedicados para terminal. Instala el servidor con SteamCMD,
lo arranca y lo para, archiva el mundo, edita los ajustes que leen los flags y te deja hacer todo
desde un panel con teclas.

Requisitos:

- Linux (usa grupos de procesos POSIX y `/proc`).
- SteamCMD en `~/.local/share/steamcmd` o en `PATH`. También puedes apuntarlo con
  `GAMEHOST_STEAMCMD=/ruta/a/steamcmd.sh`.
- Espacio en disco: Core Keeper ~600 MB, Project Zomboid ~4 GB, Don't Starve Together ~4,6 GB.
- Para Core Keeper, una sesión gráfica o `Xvfb`: algunas builds inicializan gráficos aunque usen
  `-batchmode`.

Los datos viven en `~/.local/share/gamehost` (o en `GAMEHOST_HOME`), nunca dentro del repositorio.

## 2. Instalar el programa

Desde el repositorio:

```bash
git clone https://github.com/SyberFoxx/gamehost.git
cd gamehost
make build          # deja el binario en bin/gamehost
./bin/gamehost doctor
```

`doctor` comprueba plataforma, permisos del directorio de datos, SteamCMD y pantalla. Si SteamCMD
no aparece:

```bash
mkdir -p ~/.local/share/steamcmd
curl -sqL https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz \
  | tar -xz -C ~/.local/share/steamcmd
```

Para tenerlo en el PATH:

```bash
install -m 755 bin/gamehost ~/.local/bin/gamehost
```

## 3. Abrir el TUI (lo principal)

```bash
gamehost
```

Sin argumentos y con una terminal real, abre el panel. Con una tubería o un redirect no abre:
imprime la ayuda y sale con código 2, para que un script o systemd nunca se queden esperando una
tecla.

Pantalla principal:

- `↑` `↓` (o `k` `j`): mover el cursor.
- `←` `→` (o `h` `l`): cambiar de instancia si tienes varias.
- `Enter`: ejecutar la acción o entrar en la pantalla.
- `n`: crear una instancia nueva.
- `r`: recargar el estado.
- `?`: mapa de teclas, con el comando equivalente.
- `Esc`: volver atrás.
- `q` o `Ctrl+C`: salir.

Acciones rápidas: `Start`, `Stop`, `Logs`, `Backup`.
Pantallas de instancia: `Details`, `Settings`, `Files`, `Admins`, `Backups`, `Systemd`.
Pantallas del sistema: `Environment`, `Web panel`, `New instance`, `Quit`.

Lo que hace cada pantalla:

| Pantalla | Qué hace | Comando equivalente |
| --- | --- | --- |
| New instance | asistente con validación; oculta los campos que el juego no usa | `create` |
| Details | configuración y estado de solo lectura | `show`, `status` |
| Settings | edita lo que leen los flags, incluida la memoria de Project Zomboid; exige servidor parado | `config` |
| Files | instala o actualiza con la salida de SteamCMD en vivo (`Esc` cancela) | `install`, `update` |
| Logs | últimas líneas y seguimiento en vivo; `G` vuelve al final, `n` cambia el tamaño | `logs -f`, `logs -n` |
| Backups | `b` archiva (pregunta retención), `Enter` restaura, `i` importa un mundo | `backup`, `restore`, `import-world` |
| Admins | `a` añade, `d` elimina; servidor parado | `admin add`, `admin remove` |
| Mods | Project Zomboid: `a` añade un mod, `w` un ítem del Workshop, `d` elimina | `mods add`, `mods remove` |
| Systemd | `i` instala unidades, `e` además habilita, `u` las quita, `a` edita rutas | `service install`, `service uninstall` |
| Environment | chequeos de `doctor` con la versión del binario | `doctor`, `version` |
| Web panel | panel en `127.0.0.1:8080` con la ventana de backup; `b` elige instancia a arrancar | `serve`, `serve --start` |

Las acciones destructivas (parar, actualizar, restaurar, importar, desinstalar, quitar admin)
piden confirmación.

Si ejecutas `gamehost` desde un script, `tmux` o systemd, usa los comandos; el TUI necesita una
terminal. `gamehost start` en primer plano tampoco abre TUI: el panel es dueño de la pantalla.

En primer plano, `Ctrl+C` o cerrar la ventana guardan el mundo y apagan el servidor. Con `--detach`
el servidor vive sin terminal y lo paras con `gamehost stop`.

## 4. Tu primer servidor

### Core Keeper

```bash
gamehost create partidita --max-players 4 --world-name "Partidita" --password "cambia-esto"
gamehost install partidita
gamehost start partidita --detach
gamehost status partidita     # muestra el game id para invitar
```

Los amigos entran con el **game id** de `status`. Flags útiles:

- `--port 27015`: conexión directa (necesita abrir el puerto); sin `--port` usa el relay de Steam.
- `--mode normal|hard|creative|casual` y `--seed`.
- `--game-id` para fijar un id concreto (15–28 caracteres sin `Y y x 0 O`).

### Don't Starve Together

Offline (solo LAN, sin token), con cuevas:

```bash
gamehost create cueva --game dontstarvetogether --offline --caves \
  --max-players 4 --world-name "Noche de Búhos"
gamehost install cueva
gamehost start cueva --detach
```

Online (aparece en el navegador de servidores de Klei):

1. Entra en <https://accounts.klei.com> con Steam.
2. *Don't Starve Together → Servers → Add New Server*.
3. Copia el token (`pds-g^KU_...`).

```bash
gamehost create online --game dontstarvetogether --cluster-token 'pds-g^KU_...'
gamehost install online
gamehost start online --detach
```

`status` muestra el nombre del clúster que verán los jugadores. El token se guarda en
`cluster_token.txt` con permisos `0600` y nunca viaja en los argumentos del proceso; si lo pegas en
un chat, regenéralo después y actualízalo con:

```bash
gamehost config online --cluster-token 'nuevo-token'
```

Los archivos del clúster quedan en:

```text
data/gamehost/<instancia>/
├── cluster.ini            ajustes compartidos y, con --caves, la sección [SHARD]
├── cluster_token.txt      solo online, 0600
├── adminlist.txt          IDs KU_..., uno por línea
├── Master/server.ini      superficie (UDP 10999 por defecto)
└── Caves/server.ini       cuevas (UDP 11000, solo con --caves)
```

`gamehost` crea los ini **solo si faltan**. Después son tuyos y del servidor: no se reescriben.

### Project Zomboid

```bash
gamehost create zomboid --game projectzomboid --world-name base \
  --admin-password "cambia-esto"
gamehost install zomboid
gamehost start zomboid --detach
```

Los jugadores añaden la dirección y el puerto UDP en *Favorites*: `16261` de juego y `16262`
directo por defecto. El límite de jugadores y la contraseña de entrada viven en
`data/Server/base.ini`, que pertenece al servidor.

La memoria del servidor (el *heap* de la JVM) sí la maneja `gamehost`:

```bash
gamehost create zomboid --game projectzomboid --admin-password "cambia-esto" --memory 4g
gamehost config zomboid --memory 6g     # con el servidor parado
gamehost config zomboid --memory ""     # vuelve al valor por defecto, 8g
```

Se aceptan megabytes o gigabytes (`4096m`, `6g`), con un mínimo de `512m`. El valor se escribe en
`files/ProjectZomboid64.json` antes de cada arranque, porque `update` repone el archivo original; el
resto de los argumentos de la JVM no se toca. En el TUI es el campo **Memory** del asistente y de
`Settings`, visible solo para Project Zomboid. Un heap mayor que la RAM total de la máquina se
rechaza antes de arrancar.

Mods (solo Project Zomboid):

```bash
gamehost stop zomboid
gamehost mods add zomboid 2200148440 --workshop   # ítem del Workshop
gamehost mods add zomboid Hydrocraft             # id del mod (carpeta)
gamehost mods list zomboid
gamehost mods remove zomboid Hydrocraft
gamehost start zomboid --detach
```

`mods add` y `mods remove` solo reescriben las líneas `WorkshopItems=` y `Mods=` del ini; el resto
del archivo no se toca. Hacen falta con el servidor parado porque el ini pertenece al servidor. En
el TUI, la pantalla `Mods` ofrece lo mismo con `a`, `w` y `d`.

## 5. Qué es un shard y por qué existe

Un **shard** es una copia del mundo que corre en su propio proceso. Don't Starve Together separa
la superficie (`Master`) y las cuevas (`Caves`) en dos procesos porque cada una tiene su propia
generación, Lua y guardado; el juego no puede cargar ambas en un solo proceso de servidor.

Sin el shard de cuevas, el mundo funciona pero los portales que llevan al subsuelo se quedan
desactivados. Con `--caves`, `gamehost`:

- escribe `Caves/server.ini` con `is_master = false`;
- añade la sección `[SHARD]` a `cluster.ini` (una sola vez) con la clave compartida y el puerto
  interno `10888`;
- arranca un segundo proceso en el **mismo grupo de procesos** que el Master, en el puerto UDP
  siguiente (`11000` si el Master usa `10999`);
- para ambos con una sola señal (`stop`, `Ctrl+C`, systemd) y archiva los dos guardados en el
  mismo `backup`;
- si un shard muere sin que se lo pidan, apaga el otro y marca la instancia como `crashed`, en vez
  de dejar portales rotos.

Por eso `status` sigue mostrando un solo PID: es el líder del grupo. No hay que arrancar ni parar
Caves a mano.

## 6. Administración sin entrar al juego

### Admins

Core Keeper usa Steam ID64 en `Admins.json`:

```bash
gamehost admin add partidita 76561198012345678 --name "Ramsés"
gamehost admin add partidita 76561198012345679 --name "Amigo" --limited
gamehost admin list partidita
gamehost admin remove partidita 76561198012345679
```

Don't Starve Together usa el ID de Klei (`KU_...`) en `adminlist.txt`:

```bash
gamehost admin add cueva KU_QDNly1sC
gamehost admin list cueva
gamehost admin remove cueva KU_QDNly1sC
```

En el TUI: `Admins` → `a` para añadir, `d` para quitar. El servidor debe estar parado. En DST no
hay privilegios ni nombre visible; `--limited` se rechaza para no escribir un archivo que el juego
ignoraría.

### Backups y restore

```bash
gamehost backup partidita --keep 5
gamehost restore partidita 2026-11-06_17-25-41.tar.gz
gamehost import-world partidita ~/Descargas/mi-mundo.world.gzip
```

`backup`, `restore` e `import-world` exigen servidor parado. `restore` guarda antes los datos
actuales como `pre-restore_<fecha>.tar.gz`, así que siempre hay vuelta atrás.

### Programa backups automáticos

```bash
gamehost config partidita --backup-every 24h --backup-keep 7
gamehost serve                     # la ventana la gestiona el panel
# o, sin dejar nada abierto:
gamehost service install partidita --enable
```

No uses las dos cosas a la vez para la misma instancia. La ventana para el servidor, archiva,
poda y lo vuelve a arrancar.

### Logs y estado

```bash
gamehost status partidita
gamehost logs partidita -n 200
gamehost logs partidita -f
```

Los logs se guardan en `logs/<fecha>.log`; se conservan los diez más recientes.

### Servicio systemd

```bash
gamehost service install partidita --enable
gamehost service uninstall partidita
```

Si el servidor necesita pantalla (Core Keeper), instala desde la sesión gráfica para que la unidad
herede `DISPLAY`, o usa `Xvfb`:

```bash
Xvfb :99 -screen 0 1280x720x24 &
DISPLAY=:99 gamehost service install partidita --enable
```

## 7. Panel web

```bash
gamehost serve                      # http://127.0.0.1:8080
gamehost serve --start partidita    # arranca la instancia al levantar el panel
```

Escucha solo en loopback y no tiene autenticación: si quieres acceso remoto, ponlo detrás de un
proxy inverso con autenticación. También existe el TUI → `Web panel`.

## 8. Actualizar servidores y el propio gamehost

```bash
gamehost update partidita     # archiva data/ y revalida con SteamCMD; parado
git pull && make build        # actualizar gamehost
```

`update` nunca toca un servidor en marcha y escribe
`backups/pre-update_<fecha>.tar.gz` antes de empezar.

## 9. Problemas frecuentes

| Síntoma | Causa | Solución |
| --- | --- | --- |
| `steamcmd not found` | no está en PATH | instálalo en `~/.local/share/steamcmd` o define `GAMEHOST_STEAMCMD` |
| `server is not installed` | falta `install` | `gamehost install <nombre>` |
| `instance is already running` | el servidor o un comando de datos tiene `run.lock` | espera o `gamehost stop <nombre>` |
| `backup`/`restore`/`update` se niegan | el servidor sigue vivo | páralo primero |
| DST sale con `127` y `libcurl-gnutls.so.4` | Fedora/Nobara no empaquetan esa variante | instala una build con gnutls en el loader |
| DST offline no arranca | puerto fuera de 10998–11018 | `gamehost config <nombre> --port 10999` |
| Caves no conecta | el puerto interno `10888` o los Steam `27018–27021` están ocupados | libera esos puertos |
| Core Keeper abre ventana | alguna build ignora `-batchmode` | usa sesión gráfica o `Xvfb` |
| El TUI no abre | stdin/stdout no son terminales | ejecútalo desde una terminal real |
| Amigos no entran a PZ | los puertos son UDP | abre `16261` y `16262` |

## 10. Dónde está cada cosa

```text
~/.local/share/gamehost/servers/<nombre>/
├── instance.json     configuración (0600: puede contener el token)
├── state.json        fase, pid, último código de salida
├── files/            instalación de SteamCMD
├── data/             mundo y datos del juego
├── logs/             un archivo por arranque, se conservan diez
├── backups/          archivos tar.gz y pre-restore_*.tar.gz
└── run.lock          cerrojo de la instancia
```

Para borrar una instancia por completo: `gamehost stop <nombre>` y elimina su carpeta de
`servers/`. No hay comando `delete` todavía; se hace a mano y con el servidor parado.

## 11. Seguridad

- `serve` escucha solo en `127.0.0.1`; no lo expongas sin autenticación.
- Los tokens y `instance.json` quedan en `0600`.
- El token no aparece en `ps`: se escribe en un archivo antes del arranque.
- Si un token se filtra, regenéralo en Klei y actualízalo con `gamehost config`.
- Los backups contienen el mundo y los archivos del clúster; trátalos como datos sensibles.

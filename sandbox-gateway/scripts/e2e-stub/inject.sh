# shellcheck shell=bash
# Sourced by e2e-stub/entrypoint.sh. No shebang: direct exec stays unchanged.
inject_one() {
    local spec="$1" src dest tmp="" _curl_cfg="" _h
    IFS='|' read -r src dest <<< "$spec"
    [ -n "$src" ] || return 0
    dest="${dest:-$CONFIG_ROOT}"
    case "$src" in
        http://*|https://*)
            tmp="$(mktemp)"
            echo "inject: 下载 ${src%%\?*}"   # 隐去 query，避免泄露预签名 token
            if [ -n "${SANDBOX_INJECT_HEADERS:-}" ]; then
                _curl_cfg="$(mktemp)"; chmod 600 "$_curl_cfg"
                while IFS= read -r _h; do
                    [ -n "$_h" ] || continue
                    printf 'header = "%s"\n' "$_h" >> "$_curl_cfg"
                done <<< "${SANDBOX_INJECT_HEADERS:-}"
            fi
            if ! curl -fsSL ${_curl_cfg:+-K "$_curl_cfg"} "$src" -o "$tmp"; then
                echo "inject: 下载失败，跳过 ${src%%\?*}" >&2
                [ -n "$_curl_cfg" ] && rm -f "$_curl_cfg"
                rm -f "$tmp"; return 0
            fi
            [ -n "$_curl_cfg" ] && rm -f "$_curl_cfg"
            # 按 URL 扩展名补后缀，便于下面按归档类型判定
            case "${src%%\?*}" in
                *.tar.gz|*.tgz) mv "$tmp" "${tmp}.tgz";  src="${tmp}.tgz";  tmp="${tmp}.tgz" ;;
                *.tar.bz2)      mv "$tmp" "${tmp}.tbz2"; src="${tmp}.tbz2"; tmp="${tmp}.tbz2" ;;
                *.tar.xz)       mv "$tmp" "${tmp}.txz";  src="${tmp}.txz";  tmp="${tmp}.txz" ;;
                *.tar)          mv "$tmp" "${tmp}.tar";  src="${tmp}.tar";  tmp="${tmp}.tar" ;;
                *.zip)          mv "$tmp" "${tmp}.zip";  src="${tmp}.zip";  tmp="${tmp}.zip" ;;
                *)              src="$tmp" ;;
            esac
            ;;
    esac
    if [ ! -e "$src" ]; then
        echo "inject: 源不存在，跳过 ${src}" >&2
        [ -n "$tmp" ] && rm -f "$tmp"
        return 0
    fi
    mkdir -p "$dest"
    case "$src" in
        *.tar.gz|*.tgz)   echo "inject: 解压 ${src} → ${dest}"; tar -xzf "$src" -C "$dest" || echo "inject: 解压失败 ${src}" >&2 ;;
        *.tar.bz2|*.tbz2) echo "inject: 解压 ${src} → ${dest}"; tar -xjf "$src" -C "$dest" || echo "inject: 解压失败 ${src}" >&2 ;;
        *.tar.xz|*.txz)   echo "inject: 解压 ${src} → ${dest}"; tar -xJf "$src" -C "$dest" || echo "inject: 解压失败 ${src}" >&2 ;;
        *.tar)            echo "inject: 解压 ${src} → ${dest}"; tar -xf  "$src" -C "$dest" || echo "inject: 解压失败 ${src}" >&2 ;;
        *.zip)            echo "inject: 解压 ${src} → ${dest}"; unzip -oq "$src" -d "$dest" || echo "inject: 解压失败（需 unzip）${src}" >&2 ;;
        *)
            if [ -d "$src" ]; then
                echo "inject: 复制目录 ${src}/. → ${dest}/"; cp -a "$src/." "$dest/" || echo "inject: 复制失败 ${src}" >&2
            else
                echo "inject: 复制文件 ${src} → ${dest}/"; cp -a "$src" "$dest/" || echo "inject: 复制失败 ${src}" >&2
            fi
            ;;
    esac
    [ -n "$tmp" ] && rm -f "$tmp"
    return 0
}

if [ -n "$SANDBOX_INJECT" ]; then
    echo "契约注入：处理 SANDBOX_INJECT（默认目标 CONFIG_ROOT=${CONFIG_ROOT}）…"
    IFS=',' read -ra _inject_specs <<< "$SANDBOX_INJECT"
    for _spec in "${_inject_specs[@]}"; do
        [ -n "$_spec" ] || continue
        inject_one "$_spec"
    done
    unset _inject_specs _spec
fi


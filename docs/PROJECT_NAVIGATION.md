# Điều hướng AI — theo project (qua proxy)

amux phục vụ **nhiều repo client**. Mỗi project có bản đồ riêng tại
`~/.am/workspaces/<project-name>/` — **không** dùng map repo amux khi code app khác.

---

## Init vs check/update (token)

| Lệnh | Phạm vi | Khi nào |
|------|---------|---------|
| `am map init` | **FULL** scan tree | Lần đầu / thiếu map |
| `am map update` | **FULL** regenerate cấu trúc | Đổi lớn kiến trúc (giữ `annotations.json`) |
| `am map recent` | **HẸP** git-changed ∪ touched | Check / quyết định learn |
| `am map touch` | ghi `focus.json` | Vừa mở 1 file/func |
| `am map get` | **1 dòng** | Xem summary 1 func — không đọc MODULES |
| `am map learn` | **1–N func** đã đọc sâu | Update map kiến thức |

**Init giữ nguyên (full).** Kiểm tra + enrich map **chỉ** recent/touched — không quét lại cả map/repo trong context AI.

```bash
cd /path/to/your-app
am map init                 # lần đầu ONLY
am map recent --needs-learn # shell/go — danh sách hẹp
am map touch --file pkg/auth/login.go --func Login
am map get --file pkg/auth/login.go --func Login
am map learn --file pkg/auth/login.go --func Login --summary "Issues JWT after bcrypt"
am map show
```

### Vòng học tối ưu token

1. `am map recent [--needs-learn]` (Go/git — 0 API token)  
2. Chỉ mở **source** các func trong list đó  
3. `am map learn …`  
4. **Cấm:** `cat` full `MODULES.md` / full `AI_CODEBASE_MAP.md` khi chỉ check/update  
5. Batch: `echo '[...]' | am map learn --stdin`

Proxy lần đầu thiếu map → `EnsureMapIfMissing` = init (full). Agent sau đó chỉ `recent` + `learn`.

Nội dung workspace:

| File | Nội dung |
|------|----------|
| `AI_CODEBASE_MAP.md` | stack + bảng module (sinh lúc init/update) |
| `MODULES.md` | inventory đầy đủ (máy đọc; AI dùng `recent`/`get`) |
| `ai-locate.yaml` | tasks + functions |
| `annotations.json` | learn — sống qua `am map update` |
| `focus.json` | session touched (git∪touch) |
| `AGENTS.md` | pointer |
| `project_root.txt` | absolute path repo |

---

## Agent dùng map thế nào

1. `am map show` — chỉ paths  
2. Match keywords trong `ai-locate.yaml` (hoặc `am map recent`)  
3. Mở 1–3 source file — **không** dump MODULES  
4. Đọc sâu xong → `am map learn` + `am map touch`  
5. Lần sau: `am map recent --needs-learn` trước khi đọc lại  

---

## Hai loại bản đồ

| Loại | Khi nào | File |
|------|---------|------|
| **Tool amux** | Sửa gateway amux | `amux/docs/AI_CODEBASE_MAP.md` |
| **Client project** | App qua proxy | `~/.am/workspaces/<name>/` |

---

## Proxy

- `ProjectForRemoteAddr` → git root client
- Chưa có map → tự `GenerateMap` (init)
- Admin: `GET /_am/project-nav?root=/path/to/app`

package panel

import (
	"net/http"
	"strconv"
)

func maskNodeSecrets(item map[string]any) {
	for _, field := range []string{"ssh_password", "sudo_password", "traffic_stats_secret", "obfs_password"} {
		item[field+"_saved"] = toString(item[field]) != "" || boolValue(item[field+"_saved"], false)
		item[field] = ""
	}
}
func (a *App) handleNodeSecret(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, ok := requireUser(r.Context(), a.auth, w, r)
	if !ok || !requirePermission(user, "node.manage", w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 404, "节点不存在")
		return
	}
	field := r.URL.Query().Get("field")
	switch field {
	case "ssh_password", "sudo_password", "traffic_stats_secret", "obfs_password":
	default:
		writeError(w, 400, "不支持查看此字段")
		return
	}
	node, err := a.hysteria.getStoredNode(r.Context(), id)
	if err != nil || node == nil || node.DeletedAt.Valid {
		writeError(w, 404, "节点不存在")
		return
	}
	item, err := a.hysteria.decryptNode(*node)
	if err != nil {
		writeError(w, 500, "密码读取失败")
		return
	}
	a.writeJSON(w, 200, apiEnvelope{Success: true, Data: map[string]any{"value": item[field]}})
}

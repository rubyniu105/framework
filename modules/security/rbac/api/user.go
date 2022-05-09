/* Copyright © INFINI Ltd. All rights reserved.
 * web: https://infinilabs.com
 * mail: hello#infini.ltd */

package api

import (
	"errors"
	log "github.com/cihub/seelog"
	"golang.org/x/crypto/bcrypt"
	"infini.sh/framework/core/api"
	httprouter "infini.sh/framework/core/api/router"
	"infini.sh/framework/core/security/rbac"
	"infini.sh/framework/core/util"
	"infini.sh/framework/modules/elastic"
	"net/http"
	"time"
)

func (h APIHandler) CreateUser(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var user rbac.User
	err := h.DecodeJSON(r, &user)
	if err != nil {
		h.Error400(w, err.Error())
		return
	}
	if user.Name == ""  {

		h.Error400(w, "username and phone and email is require")
		return
	}
	//localUser, err := biz.FromUserContext(r.Context())
	//if err != nil {
	//	log.Error(err.Error())
	//	h.ErrorInternalServer(w, err.Error())
	//	return
	//}
	randStr := util.GenerateRandomString(8)
	hash, err := bcrypt.GenerateFromPassword([]byte(randStr), bcrypt.DefaultCost)
	if err != nil {
		return
	}
	user.Password = string(hash)

	user.Created = time.Now()
	user.Updated = time.Now()

	id, err := h.User.Create(&user)
	user.ID = id
	if err != nil {
		_ = log.Error(err.Error())
		h.ErrorInternalServer(w, err.Error())
		return
	}
	_ = h.WriteOKJSON(w, util.MapStr{
		"_id":      id,
		"password": randStr,
		"result":   "created",
	})
	return

}

func (h APIHandler) GetUser(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.MustGetParameter("id")
	user, err := h.User.Get(id)
	if errors.Is(err, elastic.ErrNotFound) {
		h.WriteJSON(w, api.NotFoundResponse(id), http.StatusNotFound)
		return
	}

	if err != nil {
		_ = log.Error(err.Error())
		h.ErrorInternalServer(w, err.Error())
		return
	}
	h.WriteOKJSON(w, api.FoundResponse(id, user))
	return
}

func (h APIHandler) UpdateUser(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.MustGetParameter("id")
	var user rbac.User
	err := h.DecodeJSON(r, &user)
	if err != nil {
		_ = log.Error(err.Error())
		h.Error400(w, err.Error())
		return
	}
	//localUser, err := biz.FromUserContext(r.Context())
	//if err != nil {
	//	log.Error(err.Error())
	//	h.ErrorInternalServer(w, err.Error())
	//	return
	//}
	oldUser, err := h.User.Get(id)
	if err != nil {
		_ = log.Error(err.Error())
		h.ErrorInternalServer(w, err.Error())
		return
	}
	user.Updated = time.Now()
	user.Created = oldUser.Created
	user.ID = id
	err = h.User.Update(&user)

	if err != nil {
		_ = log.Error(err.Error())
		h.ErrorInternalServer(w, err.Error())
		return
	}
	_ = h.WriteOKJSON(w, api.UpdateResponse(id))
	return
}


func (h APIHandler) DeleteUser(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.MustGetParameter("id")
	//localUser, err := biz.FromUserContext(r.Context())
	//if err != nil {
	//	log.Error(err.Error())
	//	h.ErrorInternalServer(w, err.Error())
	//	return
	//}
	err := h.User.Delete(id)
	if errors.Is(err, elastic.ErrNotFound) {
		h.WriteJSON(w, api.NotFoundResponse(id), http.StatusNotFound)
		return
	}
	if err != nil {
		_ = log.Error(err.Error())
		h.ErrorInternalServer(w, err.Error())
		return
	}
	_ = h.WriteOKJSON(w, api.DeleteResponse(id))
	return
}

func (h APIHandler) SearchUser(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var (
		keyword = h.GetParameterOrDefault(r, "keyword", "")
		from    = h.GetIntOrDefault(r, "from", 0)
		size    = h.GetIntOrDefault(r, "size", 20)
	)

	res, err := h.User.Search(keyword, from, size)
	if err != nil {
		log.Error(err.Error())
		h.ErrorInternalServer(w, err.Error())
		return
	}

	h.Write(w, res.Raw)
	return

}
func (h APIHandler) UpdateUserPassword(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	id := ps.MustGetParameter("id")
	var req = struct {
		Password string `json:"password"`
	}{}
	err := h.DecodeJSON(r, &req)
	if err != nil {
		_ = log.Error(err.Error())
		h.Error400(w, err.Error())
		return
	}
	//localUser, err := biz.FromUserContext(r.Context())
	//if err != nil {
	//	log.Error(err.Error())
	//	h.ErrorInternalServer(w, err.Error())
	//	return
	//}
	user, err := h.User.Get(id)
	if err != nil {
		_ = log.Error(err.Error())
		h.ErrorInternalServer(w, err.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return
	}
	user.Password = string(hash)
	user.Updated = time.Now()
	err = h.User.Update(&user)
	if err != nil {
		_ = log.Error(err.Error())
		h.ErrorInternalServer(w, err.Error())
		return
	}

	_ = h.WriteOKJSON(w, api.UpdateResponse(id))
	return

}

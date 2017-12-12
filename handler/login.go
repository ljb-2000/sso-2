package handler

import (
	"github.com/beewit/beekit/utils"
	"github.com/beewit/beekit/utils/encrypt"
	"github.com/beewit/sso/global"

	"encoding/json"
	"fmt"
	"github.com/beewit/beekit/log"
	"github.com/beewit/beekit/utils/convert"
	"github.com/beewit/beekit/utils/enum"
	"github.com/dgrijalva/jwt-go"
	"github.com/labstack/echo"
	"strings"
	"time"
	"math/rand"
)

func Login(c echo.Context) error {
	// upload param
	mobile := c.FormValue("mobile")
	password := c.FormValue("password")
	// auth
	sql := `SELECT id, password, mobile, nickname,salt FROM account WHERE mobile = ? AND status = ?`
	rows, _ := global.DB.Query(sql, mobile, enum.NORMAL)
	if len(rows) != 1 {
		return utils.Error(c, "帐号或密码不存在", nil)
	}
	userInfo := rows[0]
	pwd := convert.ToString(userInfo["password"])
	salt := convert.ToString(userInfo["salt"])
	if encrypt.Sha1Encode(password+salt) != pwd {
		return utils.Error(c, "密码错误", nil)
	}

	token, err := GetToken(userInfo)
	if err != nil {
		global.Log.Error(err.Error())
		return utils.Error(c, "服务器异常", nil)
	}

	//记录登录日志
	go func() {
		global.DB.InsertMap("account_action_logs", utils.ActionLogs(c, enum.ACTION_LOGIN, convert.MustInt64(userInfo["id"])))
	}()

	return utils.Success(c, "操作成功", map[string]string{
		"token": token,
	})
}

func Forget(c echo.Context) error {
	mobile := c.FormValue("mobile")
	smsCode := c.FormValue("sms_code")
	password := c.FormValue("password")
	if mobile == "" {
		return utils.Error(c, "待发送短信的手机号码不能为空", nil)
	}
	if smsCode == "" {
		return utils.Error(c, "短信验证码不能为空", nil)
	}
	if password == "" {
		return utils.Error(c, "新密码不能为空", nil)
	}
	if len(password) > 16 {
		return utils.Error(c, "新密码最长不能超过20位", nil)
	}
	if !utils.CheckRegexp(password, "^[0-9a-z]{6,16}$") {
		return utils.Error(c, "新密码仅包含字母数字字符", nil)
	}
	if !utils.CheckMobile(mobile) {
		return utils.Error(c, "手机号码格式错误", nil)
	}
	if !CheckMobile(mobile) {
		return utils.Error(c, "手机号码不存在", nil)
	}
	rdSmsCode, _ := global.RD.GetString(mobile + "_sms_code")
	if strings.ToLower(rdSmsCode) != strings.ToLower(smsCode) {
		return utils.Error(c, "短信验证码错误", nil)
	}
	userInfo := GetAccountByMobile(mobile)
	if userInfo == nil {
		return utils.Error(c, "该手机用户不存在", nil)
	}
	sql := "UPDATE account SET password=?,salt=? WHERE mobile=?"
	_, err := global.DB.Update(sql, encrypt.Sha1Encode(password+smsCode), smsCode, mobile)
	if err != nil {
		return utils.Error(c, "修改密码失败，"+err.Error(), nil)
	}
	global.RD.DelKey(mobile + "_sms_code")

	//记录登录日志
	go func() {
		global.DB.InsertMap("account_action_logs", utils.ActionLogs(c, enum.ACTION_FORGET, convert.MustInt64(userInfo["id"])))
	}()

	return utils.Success(c, "修改密码成功", nil)
}

func Register(c echo.Context) error {
	mobile := c.FormValue("mobile")
	smsCode := c.FormValue("sms_code")
	password := c.FormValue("password")
	shareMobile := c.FormValue("shareMobile")
	var shareAccountId int64
	var shareAccMap map[string]interface{}

	if mobile == "" {
		return utils.Error(c, "待发送短信的手机号码不能为空", nil)
	}
	if smsCode == "" {
		return utils.Error(c, "短信验证码不能为空", nil)
	}
	if password == "" {
		return utils.Error(c, "登陆密码不能为空", nil)
	}
	if len(password) > 16 {
		return utils.Error(c, "登陆密码最长不能超过16位", nil)
	}
	if !utils.CheckRegexp(password, "^[0-9a-z]{6,16}$") {
		return utils.Error(c, "登陆密码仅包含字母数字字符", nil)
	}
	if !utils.CheckMobile(mobile) {
		return utils.Error(c, "手机号码格式错误", nil)
	}
	if CheckMobile(mobile) {
		return utils.Error(c, "手机号码已注册", nil)
	}
	if shareMobile != "" {
		shareAccMap = GetAccountByMobile(shareMobile)
		if shareAccMap == nil {
			return utils.Error(c, "邀请者手机号码未注册账号", nil)
		}
	}
	rdSmsCode, setStrErr := global.RD.GetString(mobile + "_sms_code")
	if setStrErr != nil {
		global.Log.Error("注册帐号验证码Redis存储错误：" + setStrErr.Error())
	}
	if strings.ToLower(rdSmsCode) != strings.ToLower(smsCode) {
		return utils.Error(c, "短信验证码错误", nil)
	}
	id := utils.ID()
	//判断ip是否存在邀请关系
	shareIPAccMap := getShareAccount(c)
	if shareIPAccMap == nil {
		//如果没有邀请关系，判断填写邀请者手机号是否是有效账号
		if shareAccMap != nil {
			shareAccountId = convert.MustInt64(shareAccMap["id"])
		}
	} else {
		shareAccountId = convert.MustInt64(shareIPAccMap["account_id"])
	}
	//添加邀请者id
	sql := "INSERT INTO account (id,mobile,password,salt,status,ct_time,ct_ip,share_account_id) VALUES (?,?,?,?,?,?,?,?)"
	_, err := global.DB.Insert(sql, id, mobile, encrypt.Sha1Encode(password+smsCode), smsCode, enum.NORMAL,
		time.Now().Format("2006-01-02 15:04:05"), c.RealIP(), shareAccountId)
	if err != nil {
		return utils.Error(c, "注册失败，"+err.Error(), nil)
	}

	global.RD.DelKey(mobile + "_sms_code")

	//记录登录日志
	go func() {
		global.DB.InsertMap("account_action_logs", utils.ActionLogs(c, enum.ACTION_REGISTER, id))
	}()

	return utils.Success(c, "注册成功", nil)
}

func GetRand() string {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("%04v", rnd.Int31n(10000))
}

func RegSendSms(c echo.Context) error {
	mobile := c.FormValue("mobile")
	code := c.FormValue("code")
	t := c.FormValue("type")
	if mobile == "" {
		return utils.Error(c, "待发送短信的手机号码不能为空", nil)
	}
	if code == "" {
		return utils.Error(c, "待发送短信需要的图形验证码不能为空", nil)
	}

	if !utils.CheckMobile(mobile) {
		return utils.Error(c, "手机号码格式错误", nil)
	}
	//图形码校验
	imgCode := global.Session(c).GetValue(global.IMG_CODE) // global.RD.GetString(global.IMG_CODE)
	if imgCode != code {
		return utils.Error(c, "图形验证码错误", nil)
	}
	//短信接口数量限制或预警
	if t == "reg" {
		//注册帐号限制
		if CheckMobile(mobile) {
			return utils.Error(c, "手机号码已注册", nil)
		}
	}
	smsCode := GetRand()
	templateParam := fmt.Sprintf(global.SMS_TEMPLATE_PARAM, smsCode)
	smsClient := utils.NewSmsClient(global.SmsGatewayUrl)
	result, err := smsClient.Execute(
		global.SmsAccessKeyId,
		global.SmsAccessKeySecret,
		mobile,
		global.SmsSignName,
		global.SMS_TEMPLATE_REG,
		templateParam)
	if err != nil {
		fmt.Println("error:", err.Error())
		return utils.Error(c, "发送失败"+err.Error(), nil)
	} else {
		resultCode := fmt.Sprintf("%v", result["Code"])
		if resultCode == "OK" {
			_, setStrErr := global.RD.SetAndExpire(mobile+"_sms_code", smsCode, global.SMS_CODE_EXPIRE)
			if setStrErr != nil {
				global.Log.Error("注册帐号验证码Redis存储错误：" + setStrErr.Error())
				return utils.Error(c, "短信发送失败", nil)
			}
			return utils.Success(c, "短信发送成功", nil)
		} else {
			return utils.Error(c, "短信发送失败", nil)
		}
	}

}

func GetToken(account map[string]interface{}) (string, error) {
	accountMap := make(map[string]interface{})
	accountMap["id"] = account["id"]
	accountMap["random"] = utils.ID()
	accStr, _ := json.Marshal(accountMap)

	token := jwt.New(jwt.SigningMethodHS256)
	tk, err := token.SignedString(accStr)
	if err != nil {
		return "", err
	}
	alt := "account_login_token" + convert.ToString(account["id"])
	//清除该帐号下的其他Token
	oldToken, err2 := global.RD.GetString(alt)
	if err2 == nil {
		global.RD.DelKey(oldToken)
	}
	//Redis 7天
	global.RD.SetAndExpire(tk, accStr, 7*12*60*60)
	global.RD.SetAndExpire(alt, tk, 7*12*60*60)
	return tk, nil

}

func CheckRegMobile(c echo.Context) error {
	mobile := c.FormValue("mobile")
	if mobile == "" || !utils.CheckMobile(mobile) {
		return utils.Success(c, "", nil)
	}
	if CheckMobile(mobile) {
		return utils.Error(c, "手机号码已注册", nil)
	}
	return utils.Success(c, "", nil)
}

func CheckMobile(mobile string) bool {
	if mobile == "" {
		return false
	}
	sql := `SELECT mobile FROM account WHERE mobile = ? `
	rows, err := global.DB.Query(sql, mobile)
	if err != nil {
		return false
	}
	if len(rows) >= 1 {
		return true
	}
	return false
}

func GetAccountByMobile(mobile string) map[string]interface{} {
	if mobile == "" {
		return nil
	}
	sql := `SELECT * FROM account WHERE mobile = ? LIMIT 1 `
	rows, err := global.DB.Query(sql, mobile)
	if err != nil {
		return nil
	}
	if len(rows) != 1 {
		return nil
	}
	return rows[0]
}

func CheckLoginToken(c echo.Context) error {
	token := c.FormValue("token")
	m, err := CheckToken(token)
	if err != nil {
		log.Logger.Error(err.Error())
		return utils.AuthFail(c, "登录已失效")
	}
	if m == nil {
		return utils.AuthFail(c, "登录已失效")
	}
	return utils.Success(c, "登录成功", m)
}

func CheckToken(token string) (map[string]interface{}, error) {
	if token == "" {
		return nil, nil
	}
	tv, err := global.RD.GetString(token)
	if err != nil {
		global.Log.Error(err.Error())
		return nil, err
	}
	if tv == "" {
		return nil, nil
	}
	var m map[string]interface{}
	err = json.Unmarshal([]byte(tv), &m)
	if err != nil {
		global.Log.Error(err.Error())
		return nil, err
	}
	id := convert.ToString(m["id"])
	sql := `SELECT id, mobile, nickname, photo, gender FROM account WHERE id=? AND status = ? LIMIT 1`
	rows, _ := global.DB.Query(sql, id, enum.NORMAL)
	if len(rows) != 1 {
		global.Log.Warning("ID:%v，登陆帐号异常", id)
		return nil, nil
	}
	//更新登录token时间
	alt := "account_login_token" + id
	global.RD.SetAndExpire(token, tv, 7*12*60*60)
	global.RD.SetAndExpire(alt, token, 7*12*60*60)

	return rows[0], nil
}

func DeleteToken(c echo.Context) error {
	token := c.FormValue("token")
	if token == "" {
		return utils.ErrorNull(c, "token错误")
	}
	global.RD.DelKey(token)
	return utils.SuccessNullMsg(c, "退出成功")
}

func GetShareAccount(c echo.Context) error {
	return utils.SuccessNullMsg(c, getShareAccount(c) != nil)
}

func getShareAccount(c echo.Context) map[string]interface{} {
	sql := "SELECT * FROM download_access_log WHERE ip=? ORDER BY ct_time DESC LIMIT 1"
	rows, _ := global.DB.Query(sql, c.RealIP())
	if len(rows) > 0 {
		return rows[0]
	}
	return nil
}

package main

import (
	"bufio"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"golang.org/x/image/bmp"

	"github.com/wonderivan/logger"

	_ "github.com/go-sql-driver/mysql"
)

//定义全局变量
var (
	web_port     int
	db_userName  string
	db_password  string
	db_Ip        string
	db_Port      string
	db_Name      string
	filepathCode int
)

type Resp struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

// 根据文件名，段名，键名获取ini的值
func getValue(filename, expectSection, expectKey string) string {
	// 打开文件
	file, err := os.Open(filename)
	// 文件找不到，返回空
	if err != nil {
		return ""
	}
	// 在函数结束时，关闭文件
	defer file.Close()
	// 使用读取器读取文件
	reader := bufio.NewReader(file)
	// 当前读取的段的名字
	var sectionName string
	for {
		// 读取文件的一行
		linestr, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		// 切掉行的左右两边的空白字符
		linestr = strings.TrimSpace(linestr)
		// 忽略空行
		if linestr == "" {
			continue
		}
		// 忽略注释
		if linestr[0] == ';' {
			continue
		}
		// 行首和尾巴分别是方括号的，说明是段标记的起止符
		if linestr[0] == '[' && linestr[len(linestr)-1] == ']' {
			// 将段名取出
			sectionName = linestr[1 : len(linestr)-1]
			// 这个段是希望读取的
		} else if sectionName == expectSection {
			// 切开等号分割的键值对
			pair := strings.Split(linestr, "=")
			// 保证切开只有1个等号分割的简直情况
			if len(pair) == 2 {
				// 去掉键的多余空白字符
				key := strings.TrimSpace(pair[0])
				// 是期望的键
				if key == expectKey {
					// 返回去掉空白字符的值
					return strings.TrimSpace(pair[1])
				}
			}
		}
	}
	return ""
}

//初始化全局变量
func readConfigFile() {
	web_port, _ = strconv.Atoi(getValue("config.ini", "webserver", "port"))
	db_userName = getValue("config.ini", "mysql", "userName")
	db_password = getValue("config.ini", "mysql", "password")
	db_Ip = getValue("config.ini", "mysql", "ip")
	db_Port = getValue("config.ini", "mysql", "port")
	db_Name = getValue("config.ini", "mysql", "dbName")
	filepathCode, _ = strconv.Atoi(getValue("config.ini", "general", "filePathCode"))
}

//Db数据库连接池
func conDb(id string) {
	//构建连接："用户名:密码@tcp(IP:端口)/数据库?charset=utf8"
	path := strings.Join([]string{db_userName, ":", db_password, "@tcp(", db_Ip, ":", db_Port, ")/", db_Name, "?charset=utf8"}, "")

	//打开数据库,前者是驱动名，所以要导入： _ "github.com/go-sql-driver/mysql"
	DB, err := sql.Open("mysql", path)
	defer DB.Close()
	if err != nil {
		//logger.Painc(err)
		return
	}
	//设置数据库最大连接数
	DB.SetConnMaxLifetime(100)
	//设置上数据库最大闲置连接数
	DB.SetMaxIdleConns(10)
	//验证连接
	if err := DB.Ping(); err != nil {
		logger.Debug("open database fail")
		return
	}
	logger.Debug("connnect success")
	logger.Debug("获取存取路径....")
	//1、获取存取路径
	sql_pathstr := fmt.Sprintf("select ip, s_virtual_dir from study_location where n_station_code = %d", filepathCode)
	filepath := query(DB, sql_pathstr)
	ip := filepath["ip"]
	s_virpath := filepath["s_virtual_dir"]
	//2、根据doctor_id查询ca数据
	sql_doctoridstr := fmt.Sprintf("select ca_value from doctor_sign where doctor_id = '%s'", id)
	caValue := query(DB, sql_doctoridstr)
	//3、解压base64
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(caValue["ca_value"]))
	m, formatString, err := image.Decode(reader)
	if err != nil {
		log.Fatal(err)
	}
	bounds := m.Bounds()
	logger.Debug(bounds, formatString)
	DX := bounds.Dx()
	DY := bounds.Dy()

	alpha := image.NewAlpha(image.Rect(0, 0, DX, DY))
	for x := 0; x < DX; x++ {
		for y := 0; y < DY; y++ {
			alpha.Set(x, y, color.Alpha{255}) //设定alpha图片的透明度
		}
	}
	//jpeg.Encode(file2, alpha, nil)

	newImg := image.NewNRGBA(m.Bounds())
	draw.Draw(newImg, newImg.Bounds(), alpha, alpha.Bounds().Min, draw.Over)
	draw.Draw(newImg, newImg.Bounds(), m, bounds.Min, draw.Over)

	//****************************************************************************************
	check(err)
	saveFilePath := fmt.Sprintf("\\\\%s\\%s\\%s.%s", ip, s_virpath, id, formatString)
	logger.Debug("保存的文件路径是：" + saveFilePath)
	//imgfile, _ := os.Create("test.png")
	imgfile, _ := os.Create(saveFilePath)
	defer imgfile.Close()
	//	err = jpeg.Encode(imgfile, newImg, &jpeg.Options{100})
	if formatString == "png" {
		err = png.Encode(imgfile, newImg)
	} else if formatString == "jpeg" || formatString == "jpg" {
		err = jpeg.Encode(imgfile, newImg, &jpeg.Options{100})
	} else {
		err = bmp.Encode(imgfile, newImg)
	}
	//err = ioutil.WriteFile(saveFilePath, cadate, 0666)
	if err != nil {
		logger.Error("生成文件错误")
	}
	logger.Debug("签名文件生成成功：" + saveFilePath)
	//更新数据库
	logger.Debug("签名生成成功，开始更新数据库....")

	update(DB, id, formatString)
	DB.Close()
}

//只能保存一条数据
func query(db *sql.DB, sqlstr string) map[string]string {
	rows, err := db.Query(sqlstr)
	defer rows.Close()
	check(err)
	record := make(map[string]string)
	for rows.Next() {
		columns, _ := rows.Columns()
		scanArgs := make([]interface{}, len(columns))
		values := make([]interface{}, len(columns))
		for i := range values {
			scanArgs[i] = &values[i]
		}
		//将数据保存到 record 字典
		err = rows.Scan(scanArgs...)
		for i, col := range values {
			if col != nil {
				record[columns[i]] = string(col.([]byte))
			}
		}
	}
	return record
}

//更新ca数据更加doctor_id
func update(db *sql.DB, id, catype string) {
	stmt, err := db.Prepare("UPDATE doctor_sign set path=?, location_code=? WHERE doctor_id=?")
	defer stmt.Close()
	check(err)
	path := fmt.Sprintf("%s.%s", id, catype)
	res, err := stmt.Exec(path, filepathCode, id)
	check(err)

	num, err := res.RowsAffected()
	check(err)
	logger.Debug("更新数据的数目是: %d", num)
	stmt.Close()
}

//接收x-www-form-urlencoded类型的post请求或者普通get请求
func caSignature(writer http.ResponseWriter, request *http.Request) {
	logger.Debug("接收到请求，开始处理请求......")
	request.ParseForm()
	doctorid, idErr := request.Form["doctor_id"]
	caType, typeErr := request.Form["type"]
	var result Resp
	if !idErr || !typeErr {
		result.Code = "401"
		result.Msg = "传入参数错误"
	} else if doctorid[0] != "" && caType[0] != "" {
		result.Code = "200"
		result.Msg = doctorid[0]
	} else {
		result.Code = "401"
		result.Msg = "参数是空字符串"
	}
	if err := json.NewEncoder(writer).Encode(result); err != nil {
		logger.Fatal(err)
	}
	go conDb(doctorid[0])
}

var mux map[string]func(http.ResponseWriter, *http.Request)

//启动webServer
func startWebServer() {
	addr := fmt.Sprintf(":%d", web_port)
	server := http.Server{
		Addr:    addr,
		Handler: &myHandler{},
	}
	mux = make(map[string]func(http.ResponseWriter, *http.Request))
	mux["/api/CASignature"] = caSignature
	server.ListenAndServe()
}

type myHandler struct{}

func (*myHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h, ok := mux[r.URL.String()]; ok {
		h(w, r)
		return
	}
	io.WriteString(w, "My server: "+r.URL.String())
}

func main() {
	//1、记录日志配置
	// 通过配置参数直接配置
	logger.SetLogger(`{"Console": {"level": "DEBG"}}`)
	// 通过配置文件配置
	logger.SetLogger("log.json")
	//2、读取配置文件
	readConfigFile()
	//3、启动webservice
	logger.Debug("....启动web服务....")
	startWebServer()
}

func check(err error) {
	if err != nil {
		logger.Error(err)
		panic(err)
	}
}

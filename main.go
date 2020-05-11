package main

import (
  "net"
  "fmt"
  "bufio"
  "strings"
  "strconv"
  "regexp"
  "sync"
  "encoding/binary"
  "time"
)

const (
  // Http11MasMax - количество ответов на запрос HTTP/1.1
  Http11MasMax = 10
)

var (
  Port string = ":8081"
  MAX_REQUEST int = 100 // предел запросов
  TIME_LIMIT int64 = 60 // лимит времени (секунды)
  TIME_WAIT int64 = 120 // ожидание после ограничения
)

type FilterRequest struct {
  sync.Mutex
  adr_time map[uint32][]int64
}

func NewFilter() *FilterRequest {
  return &FilterRequest{adr_time: make(map[uint32][]int64)}
}

func GetHtmlPage(RT int)(html string){
  var htmlList []string
  if RT == 200 {
    htmlList = []string {
      `<!DOCTYPE html>`,
      `<html>`,
      `<head>`,
      `<meta charset="utf-8">`,
      `<title> TEST </title>`,
      `</head>`,
      `<body>`,
      `TEST`,
      `</body>`,
      `</html>`,
    }
  } else if RT == 429 {
    htmlList = []string {
      `<!DOCTYPE html>`,
      `<html>`,
      `<head>`,
      `<meta charset="utf-8">`,
      `<title> Too Many Requests </title>`,
      `</head>`,
      `<body>`,
      `<h1>Too Many Requests</h1>`,
      `<p>I only allow `+
      strconv.Itoa(MAX_REQUEST) +
      ` requests per ` + strconv.Itoa(int(TIME_LIMIT)) +
      ` seconds to this Web site per logged in user.  Try again soon.</p>`,
      `</body>`,
      `</html>`,
    }
  }
  for i:=0; i<len(htmlList); i++ {
    html = html+ htmlList[i]
  }
  return
}

func Http11 (RT int)([Http11MasMax]string) {
  var http11 [Http11MasMax]string
  html := GetHtmlPage(RT)
  if RT == 428 {
    http11[0] = "HTTP/1.1 428 Precondition Required"
    http11[1] = "Content-Type:text/html;charset=UTF-8"
  } else if RT == 429 {
    http11[0] = "HTTP/1.1 429 Too Many Requests"
    http11[1] = "Content-Type:text/html;charset=UTF-8"
    http11[2] = "Retry-After: "+strconv.Itoa(int(TIME_WAIT))
    http11[4] = ""
    http11[5] = html
    http11[3] = "Content-Length: " + strconv.Itoa(len(http11[5]))
  } else if RT == 200 {
    http11[0] = "HTTP/1.1 200 OK"
    http11[1] = "Host: localhost"
    http11[2] = "Content-Type:text/html;charset=UTF-8"
    http11[4] = "Connection: close"
    http11[5] = ""
    http11[6] = html
    http11[3] = "Content-Length: " + strconv.Itoa(len(http11[6]))
  } else if RT == 404 {
    http11[0] = "404 Not Found"
  } else if RT == 304 {
    http11[0] = "304 Not Modified"
  }
  return http11
}

func sender (conn net.Conn, RT int)(){
  MassVar := Http11(RT)
  MassVar0 := MassVar[0:len(MassVar)]
  for k := 0; k < len(MassVar0); k++ {
    conn.Write([]byte(string(MassVar0[k] +"\n")))
  }
}

func listener (conn net.Conn, RequestType chan int, FR *FilterRequest)(){
  defer func() {
    //fmt.Printf("Закрываем соединение с %v \n", conn.RemoteAddr())
    conn.Close()
  }()
  ReaderConn := bufio.NewReader(conn)                                                     // слушаем сообщения
  scanr := bufio.NewScanner(ReaderConn)
  for {
    scanned := scanr.Scan()
    if !scanned {
      if err := scanr.Err(); err != nil {
        //fmt.Printf("%v(%v)", err, conn.RemoteAddr())
      }
      break
    }
    //fmt.Println("LOG  ", scanr.Text())
    if strings.Contains(scanr.Text(), "X-Forwarded-For") {                                // ищем (X-Forwarded-For)
      ms:=scanr.Text()
      re, _ := regexp.Compile(`([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})`)   // Получаем IP адрес
      result:= re.FindStringSubmatch(ms)
      if net.ParseIP(result[0]).To4() == nil {                                            // Проверка на IP
        sender(conn, 428)
      }
      tmp_buf0 := make([]byte, 8)                                                         // TODO временный буфер для хранения IP
      tmp_buf1 := make([]byte, 8)                                                         // TODO временный буфер для хранения MASK
      for i:=1; i<5; i++ {                                                                // записываем в буфер IP
        ui64, err := strconv.ParseUint(result[i], 10, 8)
        if err == nil {
          tmp_buf0[i-1] = byte(ui64)
        } else {
          fmt.Println(err)
        }
      }
      copy(tmp_buf1, tmp_buf0)
      tmp_buf1[3] = 0                                                                     // записываем в буфер MASK
      mask := binary.BigEndian.Uint32(tmp_buf1)                                           // преобразуем MASK в число
      time_now := time.Now().Unix()                                                       // получаем текущее время
      FR.Lock()
      list_time, ok := FR.adr_time[mask]                                                  // проверяем есть ли запись в буфере адрессов
      if !ok {                                                                            // если нет записи в буфере адрессов
        FR.adr_time[mask] = append(FR.adr_time[mask], time_now)                           // добавляем запись в буфер
        sender(conn, 200)
      } else {                                                                            // если есть запись в буфере адрессов
        len_list_time := len(list_time)
        if len_list_time < MAX_REQUEST {                                                  // если запросов было меньше лимита
          for i:=0; i<len_list_time; i++ {
            if time_now > list_time[i] + TIME_LIMIT {                                     // TODO если время запроса не актуально
              if i != len_list_time - 1 {                                                 // если не последний запрос
                continue
              } else {
                FR.adr_time[mask][0] = time_now                                           // добавляем время запроса
                FR.adr_time[mask] = FR.adr_time[mask][0:1]
                sender(conn, 429)
              }
            } else {                                                                      // если время запроса актуально
              if i == 0 {                                                                 // если все запросы актуальны
                FR.adr_time[mask] = append(FR.adr_time[mask], time_now)                   // добавляем время запроса
                sender(conn, 200)
                break
              } else {                                                                    // если есть хотя бы 1 не актуальный запрос
                copy(FR.adr_time[mask][0:], FR.adr_time[mask][i:])
                FR.adr_time[mask][len_list_time - i] = time_now                           // добавляем время запроса
                for j:= len_list_time - 1; j>i; j-- {                                     // удаляем все неактуальные запросы
                  FR.adr_time[mask] = FR.adr_time[mask][:len_list_time - 1]
                }
                sender(conn, 200)
                break
              }
            }
          }
        } else {                                                                          // если лимит исчерпан
          if time_now <= list_time[0] + TIME_WAIT {                                       // если время первого запроса актуально
            if list_time[len_list_time-1] == 0 {                                          // если время последнего запроса не действительно
              FR.adr_time[mask][0] = time_now + TIME_WAIT
              sender(conn, 429)
            } else {
              FR.adr_time[mask][0] = time_now + TIME_WAIT
              FR.adr_time[mask][len_list_time-1] = 0                                      // очищаем время последнего запроса
              sender(conn, 429)
            }
          } else {                                                                        // если время первого запроса не актульно
            for i:=0; i<len_list_time; i++ {
              if time_now > list_time[i] + TIME_LIMIT {                                   // если время [i] запроса не актуально
                if i != len_list_time - 1 {                                               // если не последний запрос
                  continue
                } else {
                  FR.adr_time[mask][0] = time_now                                         // добавляем время запроса
                  FR.adr_time[mask] = FR.adr_time[mask][0:1]
                }
                sender(conn, 200)
              } else {                                                                    // если время запроса актуально
                copy(FR.adr_time[mask][0:], FR.adr_time[mask][i:])
                FR.adr_time[mask][len_list_time - i] = time_now                           // добавляем время запроса
                for j:= len_list_time - 1; j>i; j-- {                                     // удаляем все неактуальные запросы
                  FR.adr_time[mask] = FR.adr_time[mask][:len_list_time - 1]
                }
                sender(conn, 200)
                break
              }
            }
          }
        }
      }
      FR.Unlock()
    }
  }
}

func GetTcp () {
  //var status_page int
  RequestType := make(chan int)
  FR := NewFilter()                                                                               // создаем буфер адрессов
  ln, err := net.Listen("tcp", Port)                                                                // устанавливаем прослушивание порта
  if err != nil {
    fmt.Println(err)
  }
  defer ln.Close()
  for {
    conn, err := ln.Accept()                                                                      // открываем порт
    if err != nil {                                                                               // если не удалось открыть порт
      fmt.Println("Error listening:", err.Error())                                                // TODO вывод ошибки
      continue
    }
    go func() {
      for {
        RT := <- RequestType
        //fmt.Println(RT)
        MassVar := Http11(RT)
        MassVar0 := MassVar[0:len(MassVar)]
        for k := 0; k < len(MassVar0); k++ {
          //fmt.Println(string(MassVar0[k]))
          conn.Write([]byte(string(MassVar0[k] +"\n")))
        }
      }
    }()
    //fmt.Printf("Открываем соединение %v \n", conn.RemoteAddr())
    go listener(conn, RequestType, FR)
  }
}

func main() {
  fmt.Println("Starting server...")
  // Создаем tcp подключение
  GetTcp()
}


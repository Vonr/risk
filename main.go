package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/mod/semver"
)

func init() {
	flag.StringVar(&token, "t", "", "Bot Token")
	flag.Parse()
}

var token string
var isReady = false

var db *sql.DB
var dbVersion = "1.0.0"
var botName = "rich"

var commands map[string]func(*discordgo.Session, *discordgo.MessageCreate, []string) = map[string]func(*discordgo.Session, *discordgo.MessageCreate, []string){
	"commands":     help,
	"help":         help,
	"h":            help,
	"cmds":         help,
	"cmd":          help,
	"daily":        daily,
	"d":            daily,
	"prefix":       prefix,
	"aliases":      alts,
	"alternatives": alts,
	"alts":         alts,
	"alt":          alts,
	"balance":      balance,
	"bal":          balance,
	"money":        balance,
	"top":          top,
	"leaderboard":  top,
	"lb":           top,
	"50/50":        fiftyfifty,
	"fiftyfifty":   fiftyfifty,
	"5050":         fiftyfifty,
}
var cmdDescs map[string]string = map[string]string{
	"commands":          "Displays a list of commands.",
	"prefix [prefix]":   "Shows or sets the current prefix.",
	"aliases [command]": "Shows all aliases for the command.",
	"balance [user]":    "Displays the amount of money the user has.",
	"daily":             "Claim your daily supply of money.",
	"top [page]":        "Shows the top players.",
	"50/50 [bet]":       "50% chance of winning, how lucky are you?",
}
var aliases [][]string = [][]string{
	{"commands", "help", "h", "cmds", "cmd"},
	{"prefix"},
	{"aliases", "alternatives", "alt", "alts"},
	{"balance", "bal", "money"},
	{"daily", "d"},
	{"top", "leaderboard", "lb"},
	{"50/50", "5050", "fiftyfifty"},
}

func main() {
	if token == "" {
		fmt.Println("No token provided. Please run: risk -t <bot token>")
		return
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalln("Error creating Discord session:", err)
	}

	dg.AddHandler(messageCreate)
	dg.AddHandler(ready)

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsGuildMembers | discordgo.IntentsGuilds

	err = dg.Open()
	if err != nil {
		log.Fatalln("Error opening connection:", err)
	}

	err = initDB()
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	dg.Close()
	db.Close()
}

func initDB() error {
	isNew := false

	if _, err := os.Stat("sqlite.db"); err == nil {
		fmt.Println("Existing sqlite.db found, proceeding")
	} else {
		fmt.Println("No existing sqlite.db found, creating...")
		file, err := os.Create("sqlite.db")
		file.Close()
		if err != nil {
			return errors.New("Could not create sqlite.db: " + err.Error())
		}
		isNew = true
		fmt.Println("Created new sqlite.db")
		fmt.Println("Initializing sqlite.db with SQLite3")
	}

	db, _ = sql.Open("sqlite3", "./sqlite.db")

	if isNew {
		err := initTables(db)
		if err != nil {
			log.Fatalln("Could not initialize tables:", err.Error())
		}
	}

	fmt.Println("Getting last version of database")
	row, err := db.Query("SELECT version FROM metadata WHERE name=?", botName)
	if err != nil {
		return populateMetadata()
	}
	defer row.Close()
	for row.Next() {
		var lastVersion string
		row.Scan(&lastVersion)

		if semver.Compare(dbVersion, lastVersion) == -1 {
			err := updateTables()
			if err != nil {
				return errors.New("Failed to update tables from " + lastVersion + " to " + dbVersion + ": " + err.Error())
			}

			updateVersionSQL := "UPDATE metadata SET version=? WHERE name=?"
			statement, err := db.Prepare(updateVersionSQL)
			if err != nil {
				return errors.New("Could not update metadata: " + err.Error())
			}
			_, err = statement.Exec(dbVersion, botName)
			if err != nil {
				return errors.New("Could not update metadata: " + err.Error())
			}
			defer statement.Close()
		}
	}

	return nil
}

func populateMetadata() error {
	insertMetadataSQL := "INSERT INTO metadata(`name`, `version`) VALUES (?, ?)"
	statement, err := db.Prepare(insertMetadataSQL)
	if err != nil {
		return errors.New("Could not add metadata to database: " + err.Error())
	}
	_, err = statement.Exec(botName, dbVersion)
	if err != nil {
		return errors.New("Could not add metadata to database: " + err.Error())
	}
	defer statement.Close()
	return nil
}

func initTables(db *sql.DB) error {
	tableNames := []string{"metadata", "servers", "users", "cooldowns"}
	statements := []string{
		"CREATE TABLE IF NOT EXISTS `metadata` (`name` TEXT NOT NULL PRIMARY KEY, `version` TEXT NOT NULL DEFAULT '1.0.0')",
		"CREATE TABLE IF NOT EXISTS `servers` (`id` TEXT NOT NULL PRIMARY KEY, `type` TEXT NOT NULL DEFAULT 'DEFAULT', `prefix` TEXT NOT NULL DEFAULT ',');",
		"CREATE TABLE IF NOT EXISTS `users` (`id` TEXT NOT NULL PRIMARY KEY, `type` TEXT NOT NULL DEFAULT 'DEFAULT', `balance` TEXT NOT NULL DEFAULT '0', `games` INTEGER NOT NULL DEFAULT 0, `daily`, INTEGER NOT NULL DEFAULT 0);",
		"CREATE TABLE IF NOT EXISTS `cooldowns` (`user_id` TEXT NOT NULL PRIMARY KEY, `balance` INTEGER NOT NULL DEFAULT 0, `top` INTEGER NOT NULL DEFAULT 0, `blackjack` INTEGER NOT NULL DEFAULT 0, `half` INTEGER NOT NULL DEFAULT 0, `scratch` INTEGER NOT NULL DEFAULT 0);"}

	tableName := ""
	for i := 0; i < len(tableNames); i++ {
		tableName = tableNames[i]
		fmt.Println("Creating,", tableName, "table...")
		statement, err := db.Prepare(statements[i])
		if err != nil {
			return errors.New("Could not create " + tableName + " table: " + err.Error())
		}
		_, err = statement.Exec()
		if err != nil {
			return errors.New("Could not create " + tableName + " table: " + err.Error())
		}
		defer statement.Close()
		fmt.Println("Created", tableName, "table successfully.")
	}

	err := populateMetadata()
	return err
}

func updateTables() error {
	return nil
}

func ready(s *discordgo.Session, event *discordgo.Ready) {
	s.UpdateGameStatus(0, "Being developed")
	isReady = true
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if !isReady || m.Author.Bot || m.Author.ID == s.State.User.ID {
		return
	}

	c := m.Content
	lc := len(c)
	prefix := getPrefix(m.GuildID)
	command := strings.TrimPrefix(c, prefix)
	command = strings.TrimPrefix(command, "<@!"+s.State.User.ID+">")
	valid := lc > len(command)
	command = strings.TrimSpace(command)
	args := strings.Split(command, " ")
	if valid && len(args) > 0 && validCmd(args[0]) {
		commands[args[0]](s, m, args[1:])
	}
}

func validCmd(name string) bool {
	for c := range commands {
		if name == c {
			return true
		}
	}
	return false
}

func getPrefix(id string) string {
	row, err := db.Query("SELECT prefix FROM servers WHERE id=?", id)
	if err != nil {
		log.Fatalln("Could not get prefix from server:", err)
	}
	defer row.Close()
	if row.Next() {
		var prefix string
		row.Scan(&prefix)
		return prefix
	}
	insertServerSQL := "INSERT INTO servers (id, prefix) VALUES (?, ',')"
	statement, err := db.Prepare(insertServerSQL)
	if err != nil {
		log.Fatalln("Could not initialize server into db:", err)
	}
	statement.Exec(id)
	defer statement.Close()
	return ","
}

func hasPerms(s *discordgo.Session, message *discordgo.Message, perms int64) bool {
	p, err := s.State.MessagePermissions(message)
	if err != nil {
		log.Fatalln("Could not get permissions from message:", err)
	}
	return p&perms != 0
}

func prefix(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if !hasPerms(s, m.Message, discordgo.PermissionManageServer) {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" you do not have the necessary permissions to change the prefix (Manage Server).")
		return
	}
	if len(args) == 0 {
		s.ChannelMessageSend(m.ChannelID, "The current prefix is "+getPrefix(m.GuildID))
		return
	}
	p := args[0]
	if len(p) > 2 {
		s.ChannelMessageSend(m.ChannelID, "Prefix should be no longer than 2 characters! This is done to save space.")
		return
	}
	setPrefix(m.GuildID, p)
	s.ChannelMessageSend(m.ChannelID, "Prefix has been successfully changed to '"+p+"'")
}

func setPrefix(id string, prefix string) {
	stmt, err := db.Prepare("UPDATE servers SET prefix=? WHERE id=?")
	if err != nil {
		log.Fatalln("Could not change prefix of server", id, "to", prefix, ":", err)
	}
	_, err = stmt.Exec(prefix, id)
	if err != nil {
		log.Fatalln("Could not change prefix of server", id, "to", prefix, ":", err)
	}
	defer stmt.Close()
}

func fiftyfifty(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	rand.Seed(time.Now().UnixNano())
	color := 0x00ff00
	message := m.Author.Mention() + " won their 50/50! :)"
	bet := big.NewInt(0)
	balance := getBalance(m.Author.ID)
	isBetting := false
	if len(args) != 0 {
		bet = getBet(m.Author.ID, args[0])
		isBetting = bet.Cmp(big.NewInt(0)) == 1
	}
	if rand.Intn(2) == 0 {
		message = m.Author.Mention() + " lost their 50/50 :("
		color = 0xff0000
		bet.Sub(big.NewInt(0), bet)
	}

	newBalance := balance.Add(balance, bet)
	setBalance(m.Author.ID, newBalance)
	if isBetting {
		message += "\nTheir balance is now " + newBalance.String()
	}

	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{},
		Color:  color,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Results",
				Value:  message,
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Title:     "50/50",
	}
	s.ChannelMessageSendEmbed(m.ChannelID, embed)
}

func help(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	message := ""
	for cmd, desc := range cmdDescs {
		message += cmd + ": `" + desc + "`\n"
	}
	s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{},
		Color:  0xffff00,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Here are all the current commands!",
				Value:  message,
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Title:     "Commands",
	})
}

func alts(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		s.ChannelMessageSend(m.ChannelID, "You need to specify a command to look for aliases for.")
		return
	}
	q := args[0]
	for _, alts := range aliases {
		contains := false
		message := ""
		for _, alt := range alts {
			message += alt + ", "
			if q == alt {
				contains = true
			}
		}
		if contains {
			s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
				Author: &discordgo.MessageEmbedAuthor{},
				Color:  0x00ff00,
				Fields: []*discordgo.MessageEmbedField{
					{
						Name:   "Here are all the current aliases for '" + q + "'",
						Value:  strings.TrimSuffix(message, ", "),
						Inline: true,
					},
				},
				Timestamp: time.Now().Format(time.RFC3339),
				Title:     "Aliases",
			})
			return
		}
	}
	s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{},
		Color:  0xff0000,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "No aliases found for '" + q + "'",
				Value:  "Try looking up aliases for a command listed in 'commands'",
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Title:     "Aliases",
	})

}

func balance(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" has $"+getBalance(m.Author.ID).String())
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(args[0], "<@!"), ">")
	_, suc := new(big.Int).SetString(id, 10)
	if !suc {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[0]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		return
	}

	user, err := s.User(id)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[0]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		return
	}
	s.ChannelMessageSend(m.ChannelID, user.Username+"#"+user.Discriminator+" has $"+getBalance(user.ID).String())
}

func getBalance(id string) *big.Int {
	createUser(id)
	stmt, err := db.Prepare("SELECT balance FROM users WHERE id=?")
	if err != nil {
		log.Fatalln("Could not get user's balance:", err)
	}
	var balanceStr string
	err = stmt.QueryRow(id).Scan(&balanceStr)
	if err != nil {
		log.Fatalln("Could not get user's balance:", err)
	}
	balance, suc := new(big.Int).SetString(balanceStr, 10)
	if !suc {
		log.Fatalln("Could not interpret user's balance:", balanceStr)
	}
	return balance
}

func setBalance(id string, bal *big.Int) {
	createUser(id)
	stmt, err := db.Prepare("UPDATE users SET balance=? WHERE id=?")
	if err != nil {
		log.Fatalln("Could not update user's balance:", err)
	}
	_, err = stmt.Exec(bal.String(), id)
	if err != nil {
		log.Fatalln("Could not update user's balance:", err)
	}
	defer stmt.Close()
}

func createUser(id string) {
	stmt, err := db.Prepare("SELECT balance FROM users WHERE id=?")
	if err != nil {
		log.Fatalln("Could not find balance from users table:", err)
	}
	defer stmt.Close()
	var balanceStr string
	err = stmt.QueryRow(id).Scan(&balanceStr)
	if err != nil {
		stmt2, err := db.Prepare("INSERT INTO users (id, type, balance, games, daily) VALUES (?, 'DEFAULT', ?, 0, 0)")
		if err != nil {
			log.Fatalln("Could not create new user:", err)
		}
		_, err = stmt2.Exec(id, "1000")
		if err != nil {
			log.Fatalln("Could not create new user:", err)
		}
		defer stmt.Close()
	}
}

func getBet(id string, bet string) *big.Int {
	balance := getBalance(id)
	amount := big.NewInt(0)
	if strings.HasSuffix(bet, "%") {
		percentage, err := strconv.Atoi(strings.TrimSuffix(bet, "%"))
		if err != nil || percentage < 0 || percentage > 100 {
			return amount
		}
		new(big.Float).Mul(new(big.Float).SetInt(balance), big.NewFloat(float64(percentage)*0.01)).Int(amount)
	} else if bet == "all" {
		return balance
	} else if bet == "half" {
		return balance.Div(balance, big.NewInt(2))
	} else {
		amt, err := strconv.ParseFloat(bet, 64)
		if err != nil || amt < 0 {
			return amount
		}
		bi := big.NewInt(int64(amt))
		if bi.Cmp(balance) == 1 {
			return balance
		}
		amount = bi
	}

	return amount
}

func daily(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	createUser(m.Author.ID)
	stmt, err := db.Prepare("SELECT daily FROM users WHERE id=?")
	if err != nil {
		log.Fatalln("Could not get daily flag:", err)
	}
	var daily int64
	err = stmt.QueryRow(m.Author.ID).Scan(&daily)
	if err != nil {
		log.Fatalln("Could not get daily flag:", err)
	}
	defer stmt.Close()

	tmr := time.Now().AddDate(0, 0, 1)
	tmr = time.Date(tmr.Year(), tmr.Month(), tmr.Day(), 0, 0, 0, 0, tmr.Location())
	if time.Unix(daily, 0).Unix() >= time.Now().Unix() {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" you already claimed your daily supply today! Come back <t:"+fmt.Sprint(tmr.Unix())+":R>")
		return
	}
	newBal := new(big.Int).Add(getBalance(m.Author.ID), big.NewInt(100))
	setBalance(m.Author.ID, newBal)
	s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" you have claimed your daily supply of $100.\nYou now have $"+newBal.String()+" Come back <t:"+fmt.Sprint(tmr.Unix())+":R>")

	stmt2, err := db.Prepare("UPDATE users SET daily=? WHERE id=?")
	if err != nil {
		log.Fatalln("Could not update daily:", err)
	}
	_, err = stmt2.Exec(tmr.Unix(), m.Author.ID)
	if err != nil {
		log.Fatalln("Could not update daily:", err)
	}
	defer stmt.Close()
}

func top(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	stmt, err := db.Prepare("SELECT COUNT(*) FROM users")
	if err != nil {
		log.Fatalln("Could not count users:", err)
	}
	var count int64
	err = stmt.QueryRow().Scan(&count)
	if err != nil {
		log.Fatalln("Could not count users:", err)
	}
	defer stmt.Close()
	pages := int(math.Ceil(float64(count) * 0.1))

	page := 1
	if len(args) > 0 {
		page, err = strconv.Atoi(args[0])
		if err != nil {
			s.ChannelMessageSend(m.ChannelID, "Invalid page number: "+args[0])
			return
		}
		if page > pages {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Exceeded number of pages: %d/%d", page, pages))
		}
	}
	page -= 1

	stmt2, err := db.Prepare("SELECT id, balance FROM users ORDER BY balance DESC LIMIT 10 OFFSET ?")
	if err != nil {
		log.Fatalln("Could not get top users:", err)
	}
	rows, err := stmt2.Query(page * 10)
	if err != nil {
		log.Fatalln("Could not get top users:", err)
	}
	defer rows.Close()
	defer stmt2.Close()

	message := ""
	n := page*10 + 1
	for rows.Next() {
		var id string
		var bal string
		err = rows.Scan(&id, &bal)
		if err != nil {
			log.Fatalln("Could not get top users:", err)
		}
		user, err := s.User(id)
		if err != nil {
			log.Fatalln("Could not get user", id, ":", err)
		}
		message += fmt.Sprintf("%d: %s#%s \u27A4 $%s", n, user.Username, user.Discriminator, bal) + "\n"
		n++
	}

	s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{},
		Color:  0xffff00,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   fmt.Sprintf("Top %d to %d richest players", page*10+1, n-1),
				Value:  message,
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Title:     "Top Players",
	})
}

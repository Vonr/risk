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
var botName = "risk"

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
	"share":        share,
	"give":         share,
	"gift":         share,
	"50/50":        fiftyfifty,
	"fiftyfifty":   fiftyfifty,
	"5050":         fiftyfifty,
}
var cmdDescs map[string]string = map[string]string{
	"commands":              "Displays a list of commands.",
	"prefix [prefix]":       "Shows or sets the current prefix.",
	"aliases [command]":     "Shows all aliases for the command.",
	"balance [user]":        "Displays the amount of money the user has.",
	"daily":                 "Claim your daily supply of money.",
	"top [page]":            "Shows the top players.",
	"share <amount> <user>": "Shares coins with the user.",
	"50/50 [bet]":           "50% chance of winning, how lucky are you?",
}
var aliases [][]string = [][]string{
	{"commands", "help", "h", "cmds", "cmd"},
	{"prefix"},
	{"aliases", "alternatives", "alt", "alts"},
	{"balance", "bal", "money"},
	{"daily", "d"},
	{"top", "leaderboard", "lb"},
	{"share", "give", "gift"},
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

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsGuildMembers | discordgo.IntentsGuilds | discordgo.IntentsDirectMessages

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
	s.UpdateListeningStatus("@Risk help")
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
		msg, _ := s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" you do not have the necessary permissions to change the prefix (Manage Server).")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	if len(args) == 0 {
		msg, _ := s.ChannelMessageSend(m.ChannelID, "The current prefix is "+getPrefix(m.GuildID))
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	p := args[0]
	if len(p) > 2 {
		msg, _ := s.ChannelMessageSend(m.ChannelID, "Prefix should be no longer than 2 characters! This is done to save space.")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	setPrefix(m.GuildID, p)
	msg, _ := s.ChannelMessageSend(m.ChannelID, "Prefix has been successfully changed to '"+p+"'")
	goDelete(s, m.ChannelID, 3*time.Second, []string{msg.ID, m.ID})
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
	msg, _ := s.ChannelMessageSendEmbed(m.ChannelID, embed)
	goDelete(s, m.ChannelID, 2*time.Second, []string{msg.ID, m.ID})
}

func help(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	message := ""
	for cmd, desc := range cmdDescs {
		message += cmd + ": `" + desc + "`\n"
	}
	msg, _ := s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
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
	goDelete(s, m.ChannelID, 3*time.Second, []string{msg.ID, m.ID})
}

func alts(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		msg, _ := s.ChannelMessageSend(m.ChannelID, "You need to specify a command to look for aliases for.")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
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
	msg, _ := s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
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
	goDelete(s, m.ChannelID, 3*time.Second, []string{msg.ID, m.ID})
}

func balance(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) == 0 {
		msg, _ := s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" has $"+getBalance(m.Author.ID).String())
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	id, err := getId(args[0])
	if err != nil {
		msg, _ := s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[0]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}

	user, err := s.User(id)
	if err != nil {
		msg, _ := s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[0]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	createUser(s, id)
	msg, _ := s.ChannelMessageSend(m.ChannelID, user.Username+"#"+user.Discriminator+" has $"+getBalance(user.ID).String())
	goDelete(s, m.ChannelID, 3*time.Second, []string{msg.ID, m.ID})
}

func getBalance(id string) *big.Int {
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

func createUser(s *discordgo.Session, id string) (*discordgo.User, error) {
	user, err := s.User(id)
	if err != nil {
		return nil, err
	}
	stmt, err := db.Prepare("SELECT balance FROM users WHERE id=?")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	var balanceStr string
	err = stmt.QueryRow(id).Scan(&balanceStr)
	if err != nil {
		stmt2, err := db.Prepare("INSERT INTO users (id, type, balance, games, daily) VALUES (?, 'DEFAULT', ?, 0, 0)")
		if err != nil {
			return nil, err
		}
		_, err = stmt2.Exec(id, "1000")
		if err != nil {
			return nil, err
		}
		defer stmt.Close()
	}
	return user, nil
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
		new(big.Float).Mul(new(big.Float).SetInt(balance), big.NewFloat(0.5)).Int(amount)
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
	createUser(s, m.Author.ID)
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
		msg, _ := s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" you already claimed your daily supply today! Come back <t:"+fmt.Sprint(tmr.Unix())+":R>")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	newBal := new(big.Int).Add(getBalance(m.Author.ID), big.NewInt(500))
	setBalance(m.Author.ID, newBal)
	msg, _ := s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" you have claimed your daily supply of $500.\nYou now have $"+newBal.String()+" Come back <t:"+fmt.Sprint(tmr.Unix())+":R>")

	stmt2, err := db.Prepare("UPDATE users SET daily=? WHERE id=?")
	if err != nil {
		log.Fatalln("Could not update daily:", err)
	}
	_, err = stmt2.Exec(tmr.Unix(), m.Author.ID)
	if err != nil {
		log.Fatalln("Could not update daily:", err)
	}
	defer stmt.Close()
	goDelete(s, m.ChannelID, 2*time.Second, []string{msg.ID, m.ID})
}

func top(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	createUser(s, m.Author.ID)
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
			msg, _ := s.ChannelMessageSend(m.ChannelID, "Invalid page number: "+args[0])
			goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
			return
		}
		if page < 1 {
			page = 1
		}
		if page > pages {
			msg, _ := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Exceeded number of pages: %d/%d", page, pages))
			goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
			return
		}
	}
	page -= 1

	stmt2, err := db.Prepare("SELECT id, balance FROM users ORDER BY CAST(balance AS DECIMAL(100, 100)) DESC LIMIT 10 OFFSET ?")
	if err != nil {
		log.Fatalln("Could not get top users:", err)
	}
	rows, err := stmt2.Query(page * 10)
	if err != nil {
		log.Fatalln("Could not get top users:", err)
	}
	defer stmt2.Close()

	message := ""
	n := page*10 + 1
	defer rows.Close()
	for rows.Next() {
		var id string
		var bal string
		err = rows.Scan(&id, &bal)
		if err != nil {
			log.Fatalln("Could not get top users:", err)
		}
		user, err := s.User(id)
		if err != nil {
			stmt3, err := db.Prepare("DELETE FROM users WHERE id=?")
			if err != nil {
				log.Fatalln("Could not delete user:", err)
			}
			_, err = stmt3.Exec(id)
			if err != nil {
				log.Fatalln("Could not delete user:", err)
			}
			defer stmt3.Close()
			top(s, m, args)
			return
		}
		message += fmt.Sprintf("%d: %s#%s \u27A4 $%s", n, user.Username, user.Discriminator, bal) + "\n"
		n++
	}

	msg, _ := s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
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
	goDelete(s, m.ChannelID, 3*time.Second, []string{msg.ID, m.ID})
}

func share(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		msg, _ := s.ChannelMessageSend(m.ChannelID, "Invalid syntax: `share <amount> <user>`")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	id, err := getId(args[1])
	if err != nil {
		msg, _ := s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[1]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	createUser(s, id)
	if id == m.Author.ID {
		msg, _ := s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" I see what you're trying to do but I'm not going to allow it.")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	amount := getBet(m.Author.ID, args[0])
	user, err := s.User(id)
	if err != nil {
		msg, _ := s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[1]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		goDelete(s, m.ChannelID, 1*time.Second, []string{msg.ID, m.ID})
		return
	}
	taxed := new(big.Int)
	new(big.Float).Mul(new(big.Float).SetInt(amount), big.NewFloat(0.95)).Int(taxed)

	newReceiverBal := new(big.Int).Add(getBalance(id), taxed)
	newSenderBal := new(big.Int).Sub(getBalance(m.Author.ID), amount)
	setBalance(m.Author.ID, newSenderBal)
	setBalance(id, newReceiverBal)

	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{},
		Color:  0x00ff00,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name: "Coins have been shared!",
				Value: fmt.Sprintf("%s sent %s $%d ($%d after 5%% tax)\n%s\u27A4$%d\n%s\u27A4$%d",
					m.Author.Mention(), user.Mention(), amount, taxed, m.Author.Mention(), newSenderBal, user.Mention(), newReceiverBal),
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Title:     "Sharing",
	}
	msg, _ := s.ChannelMessageSendEmbed(m.ChannelID, embed)
	s.ChannelMessageSendEmbed(getDM(s, m.Author.ID).ID, embed)
	s.ChannelMessageSendEmbed(getDM(s, user.ID).ID, embed)
	goDelete(s, m.ChannelID, 3*time.Second, []string{msg.ID, m.ID})
}

func getId(mention string) (string, error) {
	id := strings.TrimSuffix(strings.TrimPrefix(mention, "<@!"), ">")
	_, err := strconv.Atoi(id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func goDelete(s *discordgo.Session, channelID string, sleep time.Duration, messages []string) {
	go func() {
		time.Sleep(sleep)
		s.ChannelMessagesBulkDelete(channelID, messages)
	}()
}

func getDM(s *discordgo.Session, userID string) *discordgo.Channel {
	ch, err := s.UserChannelCreate(userID)
	if err != nil {
		log.Fatalln("Could not create user channel:", err)
	}
	return ch
}

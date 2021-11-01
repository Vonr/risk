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
	"blackjack":    blackjack,
	"bj":           blackjack,
	"50/50":        fiftyfifty,
	"fiftyfifty":   fiftyfifty,
	"5050":         fiftyfifty,
}
var cmdDescs = map[string]string{
	"commands":              "Displays a list of commands.",
	"prefix [prefix]":       "Shows or sets the current prefix.",
	"aliases [command]":     "Shows all aliases for the command.",
	"balance [user]":        "Displays the amount of money the user has.",
	"daily":                 "Claim your daily supply of money.",
	"top [page]":            "Shows the top players.",
	"share <amount> <user>": "Shares coins with the user.",
	"blackjack <bet>":       "Play a game of blackjack.",
	"50/50 [bet]":           "50% chance of winning, how lucky are you?",
}
var aliases = [][]string{
	{"commands", "help", "h", "cmds", "cmd"},
	{"prefix"},
	{"aliases", "alternatives", "alt", "alts"},
	{"balance", "bal", "money"},
	{"daily", "d"},
	{"top", "leaderboard", "lb"},
	{"share", "give", "gift"},
	{"blackjack", "bj"},
	{"50/50", "fiftyfifty", "5050"},
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
	dg.AddHandler(interact)

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsGuildMembers | discordgo.IntentsGuilds | discordgo.IntentsDirectMessages

	err = dg.Open()
	if err != nil {
		log.Fatalln("Error opening connection:", err)
	}

	err = initDB()
	if err != nil {
		log.Fatalln(err)
	}

	rand.Seed(time.Now().UnixNano())
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
	if !autoInvalidatorRunning {
		autoInvalidatorRunning = true
		go autoInvalidator(s)
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
	id, err := getID(args[0])
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[0]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		return
	}

	user, err := s.User(id)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[0]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		return
	}
	createUser(s, id)
	s.ChannelMessageSend(m.ChannelID, user.Username+"#"+user.Discriminator+" has $"+getBalance(user.ID).String())
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

func addBalance(id string, change *big.Int) *big.Int {
	bal := getBalance(id)
	bal.Add(bal, change)
	setBalance(id, bal)
	return bal
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
		_, err = stmt2.Exec(id, "10000")
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
		percentage, err := strconv.ParseFloat(strings.TrimSuffix(bet, "%"), 64)
		if err != nil || percentage < 0 || percentage > 100 {
			return amount
		}
		new(big.Float).Mul(new(big.Float).SetInt(balance), big.NewFloat(percentage*0.01)).Int(amount)
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
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" you already claimed your daily supply today! Come back <t:"+fmt.Sprint(tmr.Unix())+":R>")
		return
	}
	newBal := addBalance(m.Author.ID, big.NewInt(2000))
	s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" you have claimed your daily supply of $2000.\nYou now have $"+newBal.String()+" Come back <t:"+fmt.Sprint(tmr.Unix())+":R>")

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
			s.ChannelMessageSend(m.ChannelID, "Invalid page number: "+args[0])
			return
		}
		if page < 1 {
			page = 1
		}
		if page > pages {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Exceeded number of pages: %d/%d", page, pages))
			return
		}
	}
	page--

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

func share(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 2 {
		s.ChannelMessageSend(m.ChannelID, "Invalid syntax: `share <amount> <user>`")
		return
	}
	id, err := getID(args[1])
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[1]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		return
	}
	createUser(s, id)
	if id == m.Author.ID {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" I see what you're trying to do but I'm not going to allow it.")
		return
	}
	amount := getBet(m.Author.ID, args[0])
	user, err := s.User(id)
	if err != nil {
		s.ChannelMessageSend(m.ChannelID, m.Author.Mention()+" "+args[1]+" is not a valid User ID.\nPlease ping the user or copy their ID and paste it.")
		return
	}
	taxed := new(big.Int)
	new(big.Float).Mul(new(big.Float).SetInt(amount), big.NewFloat(0.95)).Int(taxed)

	newSenderBal := addBalance(m.Author.ID, new(big.Int).Sub(big.NewInt(0), amount))
	newReceiverBal := addBalance(id, taxed)

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
	s.ChannelMessageSendEmbed(m.ChannelID, embed)
	s.ChannelMessageSendEmbed(getDM(s, m.Author.ID).ID, embed)
	s.ChannelMessageSendEmbed(getDM(s, user.ID).ID, embed)
}

func getID(mention string) (string, error) {
	id := strings.TrimSuffix(strings.TrimPrefix(mention, "<@!"), ">")
	_, err := strconv.Atoi(id)
	if err != nil {
		return "", err
	}
	return id, nil
}

func getDM(s *discordgo.Session, userID string) *discordgo.Channel {
	ch, err := s.UserChannelCreate(userID)
	if err != nil {
		log.Fatalln("Could not create user channel:", err)
	}
	return ch
}

var cardTypes = []string{"A", "2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K"}

type blackjackGame struct {
	deck  map[string]int
	hands [][]string
	msg   *discordgo.Message
	bet   *big.Int
	time  int64
}

var blackjackGames = make(map[string]blackjackGame)

func blackjack(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	createUser(s, m.Author.ID)
	if len(args) < 1 {
		s.ChannelMessageSend(m.ChannelID, "Invalid syntax: `blackjack <bet>`")
		return
	}
	existing, exists := blackjackGames[m.Author.ID]
	if exists {
		if time.Now().Unix()-existing.time > 10 {
			delete(blackjackGames, m.Author.ID)
		} else {
			s.ChannelMessageSend(m.ChannelID, "You already have a game in progress.")
			return
		}
	}

	bet := getBet(m.Author.ID, args[0])
	if bet.Cmp(big.NewInt(0)) != 1 {
		s.ChannelMessageSend(m.ChannelID, "You must bet more than $0.")
		return
	}
	rand.Seed(time.Now().UnixNano())

	// This blackjack will be played with 8 decks
	deck := map[string]int{
		"A":  32,
		"2":  32,
		"3":  32,
		"4":  32,
		"5":  32,
		"6":  32,
		"7":  32,
		"8":  32,
		"9":  32,
		"10": 32,
		"J":  32,
		"Q":  32,
		"K":  32,
	}

	var dealerHand = make([]string, 0)
	var playerHand = make([]string, 0)

	dealerHand = append(dealerHand, getRandomCard(&deck))
	dealerHand = append(dealerHand, getRandomCard(&deck))
	for {
		if getHandTotal(&dealerHand) >= 21 {
			deck[dealerHand[len(dealerHand)-1]]++
			dealerHand = remove(dealerHand, len(dealerHand)-1)
		} else {
			break
		}
	}

	playerHand = append(playerHand, getRandomCard(&deck))
	playerHand = append(playerHand, getRandomCard(&deck))
	if getHandTotal(&playerHand) == 21 {
		payout := new(big.Int)
		new(big.Float).Mul(new(big.Float).SetInt(bet), big.NewFloat(2)).Int(payout)
		addBalance(m.Author.ID, payout)
		s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
			Content: "",
			Embed: &discordgo.MessageEmbed{
				Author: &discordgo.MessageEmbedAuthor{},
				Color:  0x00ff00,
				Fields: []*discordgo.MessageEmbedField{
					{
						Name:   "Player",
						Value:  generateHandString(&playerHand),
						Inline: true,
					},
					{
						Name:   "Dealer",
						Value:  generateHandString(&dealerHand),
						Inline: true,
					},
					{
						Name:   "Result",
						Value:  "You got a blackjack! You now have " + getBalance(m.Author.ID).String() + "(" + bet.String() + ").",
						Inline: true,
					},
				},
				Timestamp: time.Now().Format(time.RFC3339),
				Title:     "Blackjack - You won!",
			},
		})
		return
	}

	// Generate message with discordgo components for Hit, Stand, and Forfeit that the player can interact with
	msg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Content: "",
		Embed: &discordgo.MessageEmbed{
			Author: &discordgo.MessageEmbedAuthor{},
			Color:  0xffff00,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name:   "Player",
					Value:  generateHandString(&playerHand),
					Inline: true,
				},
				{
					Name:   "Dealer",
					Value:  "`" + dealerHand[0] + "` `?`",
					Inline: true,
				},
			},
			Timestamp: time.Now().Format(time.RFC3339),
			Title:     "Blackjack",
		},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Hit",
						Style:    discordgo.SuccessButton,
						Disabled: false,
						CustomID: "bj_hit",
					},
					discordgo.Button{
						Label:    "Stand",
						Style:    discordgo.SuccessButton,
						Disabled: false,
						CustomID: "bj_stand",
					},
					discordgo.Button{
						Label:    "Forfeit",
						Style:    discordgo.DangerButton,
						Disabled: false,
						CustomID: "bj_forfeit",
					},
				},
			},
		},
	})

	if err != nil {
		log.Fatalln("Could not send message:", err)
	}

	blackjackGames[m.Author.ID] = blackjackGame{
		deck:  deck,
		hands: [][]string{playerHand, dealerHand},
		msg:   msg,
		bet:   bet,
		time:  time.Now().Unix(),
	}
}

func blackjackCont(s *discordgo.Session, i *discordgo.InteractionCreate) {
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
		Data: &discordgo.InteractionResponseData{
			Flags: 1,
		},
	})
	id := ""
	if i.GuildID != "" {
		id = i.Member.User.ID
	} else {
		id = i.User.ID
	}
	game, exists := blackjackGames[id]

	if exists && i.Message.ID == game.msg.ID {
		game.time = time.Now().Unix()
		switch i.MessageComponentData().CustomID {

		case "bj_hit":
			game.hands[0] = append(game.hands[0], getRandomCard(&game.deck))
			result := "You busted!"
			color := 0xff0000
			payout := new(big.Int)
			win := false
			var mult *big.Float
			p := getHandTotal(&game.hands[0])
			d := getHandTotal(&game.hands[1])
			if d <= 16 {
				game.hands[1] = append(game.hands[1], getRandomCard(&game.deck))
			}
			if p > 21 {
				if d > 21 {
					win = true
					mult = big.NewFloat(0)
				} else {
					win = false
					mult = big.NewFloat(-1)
				}
			} else if d == 21 || (len(game.hands[1]) == 5 && d <= 21) {
				if p == 21 || (len(game.hands[0]) == 5 && p <= 21) {
					win = true
					mult = big.NewFloat(0)
				} else {
					win = true
					mult = nil
				}
			} else if p == 21 || (len(game.hands[0]) == 5 && p <= 21) {
				win = true
				mult = big.NewFloat(1.5)
			} else if d > 21 {
				win = true
				mult = big.NewFloat(1)
				if p > 21 {
					win = true
					mult = big.NewFloat(0)
				}
			} else {
				win = false
				mult = nil
			}

			if mult != nil {
				if win {
					switch mult.Cmp(big.NewFloat(1)) {
					case 1:
						color = 0x00ff00
						result = "You got a blackjack/charlie!"
					case 0:
						color = 0x00ff00
						result = "You won"
					case -1:
						color = 0xffff00
						result = "You tied"
					}
					// Payout of bet * multiplier
					new(big.Float).Mul(new(big.Float).SetInt(game.bet), mult).Int(payout)
					addBalance(id, payout)
				} else {
					// Remove initial bet from balance
					addBalance(id, payout.Neg(game.bet))
				}
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{})
				s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel: game.msg.ChannelID,
					ID:      game.msg.ID,
					Embeds: []*discordgo.MessageEmbed{
						{
							Author: &discordgo.MessageEmbedAuthor{},
							Color:  color,
							Fields: []*discordgo.MessageEmbedField{
								{
									Name:   "Player",
									Value:  generateHandString(&game.hands[0]),
									Inline: true,
								},
								{
									Name:   "Dealer",
									Value:  generateHandString(&game.hands[1]),
									Inline: true,
								},
								{
									Name:   "Result",
									Value:  result + ", You now have " + getBalance(id).String() + " (" + payout.String() + ").",
									Inline: true,
								},
							},
							Timestamp: time.Now().Format(time.RFC3339),
							Title:     "Blackjack",
						},
					},
				})
				delete(blackjackGames, id)
			} else {

				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{})
				s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel: game.msg.ChannelID,
					ID:      game.msg.ID,
					Embeds: []*discordgo.MessageEmbed{
						{
							Author: &discordgo.MessageEmbedAuthor{},
							Color:  0xffff00,
							Fields: []*discordgo.MessageEmbedField{
								{
									Name:   "Player",
									Value:  generateHandString(&game.hands[0]),
									Inline: true,
								},
								{
									Name:   "Dealer",
									Value:  "`" + game.hands[1][0] + "` `?`",
									Inline: true,
								},
							},
							Timestamp: time.Now().Format(time.RFC3339),
							Title:     "Blackjack",
						},
					},
					Components: []discordgo.MessageComponent{
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.Button{
									Label:    "Hit",
									Style:    discordgo.SuccessButton,
									Disabled: false,
									CustomID: "bj_hit",
								},
								discordgo.Button{
									Label: "Stand",

									Style:    discordgo.SuccessButton,
									Disabled: false,
									CustomID: "bj_stand",
								},
								discordgo.Button{
									Label:    "Forfeit",
									Style:    discordgo.DangerButton,
									Disabled: false,
									CustomID: "bj_forfeit",
								},
							},
						},
					},
				})
			}

		case "bj_stand":
			result := "You lost"
			color := 0xff0000
			payout := new(big.Int)
			for {
				if getHandTotal(&game.hands[1]) <= 16 {
					game.hands[1] = append(game.hands[1], getRandomCard(&game.deck))
				} else {
					break
				}
			}
			win, mult := checkHands(&game.hands[0], &game.hands[1])
			if mult == nil {
				if getHandTotal(&game.hands[0]) == getHandTotal(&game.hands[1]) {
					mult = big.NewFloat(0)
				} else {
					win = false
				}
			}

			if win {
				if mult.Cmp(big.NewFloat(1)) >= 0 {
					color = 0x00ff00
					result = "You won"
				} else {
					color = 0xffff00
					result = "You tied"
				}
				// Payout of bet * multiplier
				new(big.Float).Mul(new(big.Float).SetInt(game.bet), mult).Int(payout)
				addBalance(id, payout)
			} else {
				// Remove initial bet from balance
				addBalance(id, payout.Neg(game.bet))
			}
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{})
			s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				Channel: game.msg.ChannelID,
				ID:      game.msg.ID,
				Embeds: []*discordgo.MessageEmbed{
					{
						Author: &discordgo.MessageEmbedAuthor{},
						Color:  color,
						Fields: []*discordgo.MessageEmbedField{
							{
								Name:   "Player",
								Value:  generateHandString(&game.hands[0]),
								Inline: true,
							},
							{
								Name:   "Dealer",
								Value:  generateHandString(&game.hands[1]),
								Inline: true,
							},
							{
								Name:   "Result",
								Value:  result + ", You now have " + getBalance(id).String() + " (" + payout.String() + ").",
								Inline: true,
							},
						},
						Timestamp: time.Now().Format(time.RFC3339),
						Title:     "Blackjack - " + result,
					},
				},
			})
			delete(blackjackGames, id)

		case "bj_forfeit":
			// Remove initial bet from balance
			addBalance(i.User.ID, new(big.Int).Neg(game.bet))
			s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				Channel: game.msg.ChannelID,
				ID:      game.msg.ID,
				Embeds: []*discordgo.MessageEmbed{
					{
						Author: &discordgo.MessageEmbedAuthor{},
						Color:  0x00ff00,
						Fields: []*discordgo.MessageEmbedField{
							{
								Name:   "Player",
								Value:  generateHandString(&game.hands[0]),
								Inline: true,
							},
							{
								Name:   "Dealer",
								Value:  generateHandString(&game.hands[1]),
								Inline: true,
							},
							{
								Name:   "Result",
								Value:  "You forfeited, You now have " + getBalance(i.User.ID).String() + "(-" + game.bet.String() + ").",
								Inline: true,
							},
						},
						Timestamp: time.Now().Format(time.RFC3339),
						Title:     "Blackjack - You forfeited",
					},
				},
			})
			delete(blackjackGames, id)
		}

	} else {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "This is not your game!",
				Flags:   2,
			},
		})
	}
}

func getRandomCard(deck *map[string]int) string {
	rand.Seed(time.Now().UnixNano())
	for {
		card := cardTypes[rand.Intn(len(cardTypes))]
		if (*deck)[card] > 0 {
			(*deck)[card]--
			return card
		}
	}
}

func generateHandString(hand *[]string) string {
	var handString string
	for _, card := range *hand {
		handString += "`" + card + "` "
	}
	return handString + "\nTotal: " + strconv.Itoa(getHandTotal(hand))
}

func getHandTotal(hand *[]string) int {
	cardValues := map[string]int{
		"2":  2,
		"3":  3,
		"4":  4,
		"5":  5,
		"6":  6,
		"7":  7,
		"8":  8,
		"9":  9,
		"10": 10,
		"J":  10,
		"Q":  10,
		"K":  10,
	}
	var total int
	var aces int
	// Ace is 11 unless it would make the total go over 21
	// Due to this, its value should only be calculated after the rest.
	for _, card := range *hand {
		if card == "A" {
			total += 11
			aces++
		} else {
			total += cardValues[card]
		}
	}
	for i := 0; i < aces; i++ {
		if total > 21 {
			total -= 10
		}
	}
	return total
}

func checkHands(player *[]string, dealer *[]string) (bool, *big.Float) {
	// If the player lost, return false
	// If the the player has a blackjack, a push or has 5 cards without busting, the player wins 1.5x the bet so 1.5 should be returned.
	// If the player's hand is a bust, the player loses all of his bet so -1 should be returned.
	p := getHandTotal(player)
	d := getHandTotal(dealer)

	if p > 21 && d > 21 {
		return true, big.NewFloat(0)
	}

	if p > 21 {
		return false, big.NewFloat(-1)
	}

	if d == 21 || (len(*dealer) == 5 && d <= 21) {
		if p == 21 || (len(*player) == 5 && p <= 21) {
			return true, big.NewFloat(0)
		}
		return false, big.NewFloat(-1)
	}

	if p == 21 {
		return true, big.NewFloat(1.5)
	}

	if len(*player) == 5 && p <= 21 {
		return true, big.NewFloat(1.5)
	}

	if p > d {
		return true, big.NewFloat(1)
	}

	if d > 21 {
		return true, big.NewFloat(1)
	}

	if p == d {
		return true, big.NewFloat(0)
	}

	//	if getHandTotal(player) == getHandTotal(dealer) {
	//		return true, big.NewFloat(0)
	//	}

	return true, nil
}

func interact(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionMessageComponent:
		if strings.HasPrefix(i.MessageComponentData().CustomID, "bj_") {
			blackjackCont(s, i)
		}
	}
}

// https://stackoverflow.com/a/37335777 by T. Claverie under CC BY-SA 4.0
func remove(s []string, i int) []string {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

var autoInvalidatorRunning = false

func autoInvalidator(s *discordgo.Session) {
	for {
		time.Sleep(time.Second)
		for id, game := range blackjackGames {
			if time.Now().Unix()-game.time > 10 {
				// Remove initial bet from balance
				addBalance(id, game.bet.Neg(game.bet))
				s.ChannelMessageEditComplex(&discordgo.MessageEdit{
					Channel: game.msg.ChannelID,
					ID:      game.msg.ID,
					Embeds: []*discordgo.MessageEmbed{
						{
							Author: &discordgo.MessageEmbedAuthor{},
							Color:  0xff0000,
							Fields: []*discordgo.MessageEmbedField{
								{
									Name:   "Player",
									Value:  generateHandString(&game.hands[0]),
									Inline: true,
								},
								{
									Name:   "Dealer",
									Value:  generateHandString(&game.hands[1]),
									Inline: true,
								},
								{
									Name:   "Result",
									Value:  "You timed out. You lost " + game.bet.String() + ", and now have " + getBalance(id).String() + ".",
									Inline: true,
								},
							},
							Timestamp: time.Now().Format(time.RFC3339),
							Title:     "Blackjack - Timeout",
						},
					},
				})
				delete(blackjackGames, id)
			}
		}
	}
}

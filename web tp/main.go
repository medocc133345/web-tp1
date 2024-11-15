package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
)

type Game struct {
    Username     string
    Difficulty   string
    Word         string
    DisplayWord  string
    AttemptsLeft int
    TriedLetters []string
    Message      string
    GameOver     bool
    Won          bool
}

type Score struct {
    Username   string    `json:"username"`
    Difficulty string    `json:"difficulty"`
    Word       string    `json:"word"`
    Won        bool      `json:"won"`
    Attempts   int       `json:"attempts"`
    Date       time.Time `json:"date"`
}

var templates *template.Template
var words map[string][]string

func main() {
   
    err := loadWords("words/words.txt")
    if err != nil {
        log.Fatalf("Erreur lors du chargement des mots : %v", err)
    }

    funcMap := template.FuncMap{
        "title":    title,
        "subtract": subtract,
    }

    templates = template.Must(template.New("").Funcs(funcMap).ParseGlob("templates/*.html"))

    mux := http.NewServeMux()
    mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
    mux.HandleFunc("/", indexHandler)
    mux.HandleFunc("/game", gameHandler)
    mux.HandleFunc("/play", playHandler)
    mux.HandleFunc("/end", endHandler)
    mux.HandleFunc("/scores", scoresHandler)
    mux.HandleFunc("/save-score", saveScoreHandler)
    mux.HandleFunc("/hint", hintHandler)
    mux.HandleFunc("/404", notFoundHandler)

    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        mux.ServeHTTP(w, r)
        if w.Header().Get("Content-Type") == "" {
            notFoundHandler(w, r)
        }
    })

    log.Println("Serveur démarré sur http://localhost:8080")
    log.Fatal(http.ListenAndServe(":8080", handler))
}

func title(s string) string {
    if len(s) == 0 {
        return s
    }
    runes := []rune(s)
    runes[0] = unicode.ToUpper(runes[0])
    return string(runes)
}

func subtract(a, b int) int {
    return a - b
}

func loadWords(filePath string) error {
    words = make(map[string][]string)
    file, err := os.Open(filePath)
    if err != nil {
        return err
    }
    defer file.Close()

    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := scanner.Text()
        parts := strings.Split(line, ":")
        if len(parts) != 2 {
            continue
        }
        difficulty := strings.TrimSpace(parts[0])
        word := strings.TrimSpace(parts[1])
        words[difficulty] = append(words[difficulty], word)
    }

    return scanner.Err()
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path != "/" {
        notFoundHandler(w, r)
        return
    }

    if r.Method == http.MethodGet {
        templates.ExecuteTemplate(w, "index.html", nil)
    } else if r.Method == http.MethodPost {
        username := strings.TrimSpace(r.FormValue("username"))
        difficulty := strings.TrimSpace(r.FormValue("difficulty"))

        if username == "" || difficulty == "" {
            templates.ExecuteTemplate(w, "index.html", "Veuillez remplir tous les champs.")
            return
        }

        word := getRandomWord(difficulty)
        displayWord := strings.Repeat("_ ", len(word))

        game := Game{
            Username:     username,
            Difficulty:   difficulty,
            Word:         strings.ToUpper(word),
            DisplayWord:  displayWord,
            AttemptsLeft: 6,
            TriedLetters: []string{},
            Message:      "",
            GameOver:     false,
            Won:          false,
        }

        sessionData, err := json.Marshal(game)
        if err != nil {
            http.Error(w, "Erreur interne du serveur", http.StatusInternalServerError)
            log.Println("Erreur de sérialisation JSON :", err)
            return
        }

        encodedData := base64.StdEncoding.EncodeToString(sessionData)

        http.SetCookie(w, &http.Cookie{
            Name:     "game",
            Value:    encodedData,
            Path:     "/",
            HttpOnly: true,
            Secure:   false, 
            SameSite: http.SameSiteStrictMode,
            Expires:  time.Now().Add(24 * time.Hour),
        })

        http.Redirect(w, r, "/game", http.StatusSeeOther)
    }
}

func gameHandler(w http.ResponseWriter, r *http.Request) {
    game, err := getGameFromCookie(r)
    if err != nil || game.GameOver {
        http.Redirect(w, r, "/", http.StatusSeeOther)
        return
    }
    templates.ExecuteTemplate(w, "game.html", game)
}

func playHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Redirect(w, r, "/game", http.StatusSeeOther)
        return
    }

    game, err := getGameFromCookie(r)
    if err != nil || game.GameOver {
        http.Redirect(w, r, "/", http.StatusSeeOther)
        return
    }

    guess := strings.ToUpper(strings.TrimSpace(r.FormValue("guess")))
    if guess == "" {
        game.Message = "Veuillez entrer une lettre ou un mot."
    } else if len(guess) == 1 && isLetter(guess) {
        if contains(game.TriedLetters, guess) {
            game.Message = "Lettre déjà essayée."
        } else {
            game.TriedLetters = append(game.TriedLetters, guess)
            if strings.Contains(game.Word, guess) {
                game.Message = "Bonne lettre!"
                game.DisplayWord = updateDisplayWord(game.Word, game.DisplayWord, guess)
                if !strings.Contains(game.DisplayWord, "_") {
                    game.GameOver = true
                    game.Won = true
                }
            } else {
                game.AttemptsLeft--
                game.Message = "Mauvaise lettre!"
                if game.AttemptsLeft <= 0 {
                    game.GameOver = true
                    game.Won = false
                }
            }
        }
    } else if len(guess) == len(game.Word) && isWord(guess) {
        if guess == game.Word {
            game.DisplayWord = strings.Join(strings.Split(game.Word, ""), " ")
            game.GameOver = true
            game.Won = true
        } else {
            game.AttemptsLeft--
            game.Message = "Mot incorrect!"
            if game.AttemptsLeft <= 0 {
                game.GameOver = true
                game.Won = false
            }
        }
    } else {
        game.Message = "Entrée invalide."
    }

    sessionData, err := json.Marshal(game)
    if err != nil {
        http.Error(w, "Erreur interne du serveur", http.StatusInternalServerError)
        log.Println("Erreur de sérialisation JSON :", err)
        return
    }

    encodedData := base64.StdEncoding.EncodeToString(sessionData)

    http.SetCookie(w, &http.Cookie{
        Name:     "game",
        Value:    encodedData,
        Path:     "/",
        HttpOnly: true,
        Secure:   false, 
        SameSite: http.SameSiteStrictMode,
        Expires:  time.Now().Add(24 * time.Hour),
    })

    if game.GameOver {
        http.Redirect(w, r, "/end", http.StatusSeeOther)
    } else {
        http.Redirect(w, r, "/game", http.StatusSeeOther)
    }
}

func endHandler(w http.ResponseWriter, r *http.Request) {
    game, err := getGameFromCookie(r)
    if err != nil || !game.GameOver {
        http.Redirect(w, r, "/", http.StatusSeeOther)
        return
    }

    templates.ExecuteTemplate(w, "end.html", game)
}

func scoresHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Redirect(w, r, "/", http.StatusSeeOther)
        return
    }

    scores, err := getScores()
    if err != nil {
        http.Error(w, "Erreur de lecture des scores.", http.StatusInternalServerError)
        log.Println("Erreur de lecture des scores :", err)
        return
    }

    templates.ExecuteTemplate(w, "scores.html", scores)
}

func saveScoreHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Redirect(w, r, "/", http.StatusSeeOther)
        return
    }

    game, err := getGameFromCookie(r)
    if err != nil || !game.GameOver {
        http.Redirect(w, r, "/", http.StatusSeeOther)
        return
    }

    score := Score{
        Username:   game.Username,
        Difficulty: game.Difficulty,
        Word:       game.Word,
        Won:        game.Won,
        Attempts:   game.AttemptsLeft,
        Date:       time.Now(),
    }

    err = saveScore(score)
    if err != nil {
        http.Error(w, "Erreur de sauvegarde du score.", http.StatusInternalServerError)
        log.Println("Erreur de sauvegarde du score :", err)
        return
    }

    http.Redirect(w, r, "/scores", http.StatusSeeOther)
}

func hintHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Redirect(w, r, "/game", http.StatusSeeOther)
        return
    }

    game, err := getGameFromCookie(r)
    if err != nil || game.GameOver {
        http.Redirect(w, r, "/game", http.StatusSeeOther)
        return
    }

    found := false
    for _, c := range game.Word {
        letter := string(c)
        if !strings.Contains(game.DisplayWord, letter) && !contains(game.TriedLetters, letter) {
            game.TriedLetters = append(game.TriedLetters, letter)
            game.DisplayWord = updateDisplayWord(game.Word, game.DisplayWord, letter)
            game.AttemptsLeft--
            game.Message = "Indice utilisé!"
            found = true
            break
        }
    }

    if !found {
        game.Message = "Aucune lettre à révéler."
    }

    if !strings.Contains(game.DisplayWord, "_") {
        game.GameOver = true
        game.Won = true
    }

    sessionData, err := json.Marshal(game)
    if err != nil {
        http.Error(w, "Erreur interne du serveur", http.StatusInternalServerError)
        log.Println("Erreur de sérialisation JSON :", err)
        return
    }

    encodedData := base64.StdEncoding.EncodeToString(sessionData)

    http.SetCookie(w, &http.Cookie{
        Name:     "game",
        Value:    encodedData,
        Path:     "/",
        HttpOnly: true,
        Secure:   false, 
        SameSite: http.SameSiteStrictMode,
        Expires:  time.Now().Add(24 * time.Hour),
    })

    if game.GameOver {
        http.Redirect(w, r, "/end", http.StatusSeeOther)
    } else {
        http.Redirect(w, r, "/game", http.StatusSeeOther)
    }
}

func getGameFromCookie(r *http.Request) (Game, error) {
    var game Game
    cookie, err := r.Cookie("game")
    if err != nil {
        return game, err
    }

    decodedData, err := base64.StdEncoding.DecodeString(cookie.Value)
    if err != nil {
        return game, err
    }

    err = json.Unmarshal(decodedData, &game)
    if err != nil {
        return game, err
    }

    return game, nil
}

func getRandomWord(difficulty string) string {
    list, exists := words[difficulty]
    if !exists || len(list) == 0 {

        list = words["facile"]
    }
    rand.Seed(time.Now().UnixNano())
    return list[rand.Intn(len(list))]
}

func isLetter(s string) bool {
    return len(s) == 1 && unicode.IsLetter(rune(s[0]))
}

func isWord(s string) bool {
    for _, c := range s {
        if !unicode.IsLetter(c) {
            return false
        }
    }
    return true
}

func contains(slice []string, item string) bool {
    for _, a := range slice {
        if a == item {
            return true
        }
    }
    return false
}

func updateDisplayWord(word, display, guess string) string {
    updated := strings.Split(display, " ")
    for i, c := range word {
        if string(c) == guess {
            updated[i] = string(c)
        }
    }
    return strings.Join(updated, " ")
}

func saveScore(score Score) error {
    scores, err := getScores()
    if err != nil {
        return err
    }

    scores = append(scores, score)

    file, err := os.OpenFile("scores/scores.json", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
    if err != nil {
        return err
    }
    defer file.Close()

    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "  ")
    err = encoder.Encode(scores)
    if err != nil {
        return err
    }

    return nil
}

func getScores() ([]Score, error) {
    var scores []Score
    file, err := os.Open("scores/scores.json")
    if err != nil {

        if os.IsNotExist(err) {
            return scores, nil
        }
        return scores, err
    }
    defer file.Close()

    decoder := json.NewDecoder(file)
    err = decoder.Decode(&scores)
    if err != nil && err.Error() != "EOF" {
        return scores, err
    }

    return scores, nil
}

func notFoundHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
    templates.ExecuteTemplate(w, "404.html", nil)
}

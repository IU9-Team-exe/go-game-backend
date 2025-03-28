conn = new Mongo();

db = conn.getDB("team_exe");

db.createCollection('users');

db.users.insertOne(
    {
        username: "artem",
        email: "artem@example.com",
        created_at: new Date(),
        updated_at: new Date(),
        rating: 1500,
        coins: 100,
        statistic: { wins: 10, losses: 5, draws: 2 },
        password_hash: "755",
        password_salt: ""
    });
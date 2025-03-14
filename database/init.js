var db = connect('localhost:27017/tofl_gpt_chat');

db.createCollection("item");
db.createCollection("user_achievement");


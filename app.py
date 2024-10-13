import os
import time

app = Flask(__name__)

@app.route('/health', methods=['HEAD', 'GET'])
def health_check():
    # TODO should add more logic here to check the application's health,
    # such as database connectivity, external service status, etc.
    return '', 200  

@app.route('/long_task')
def long_running():
    app_name = os.getenv('APP_NAME', 'app')
    print("starting the work")
    time.sleep(5)
    print("finished")

    return f'{app_name}! \n'

@app.route('/')
def hello():
    app_name = os.getenv('APP_NAME', 'app')
    print("starting the work")
    print("finished")

    return f'{app_name}! \n'

if __name__ == '__main__':
    port = int(os.getenv('PORT', 5000))
    app.run(host='0.0.0.0', port=port)

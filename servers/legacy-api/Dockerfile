FROM python:3.12-alpine
WORKDIR /app
COPY servers/legacy-api/ ./
COPY specs/ ./specs/
EXPOSE 9002
CMD ["python", "main.py"]
